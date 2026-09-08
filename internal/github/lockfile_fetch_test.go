package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

// blobServer answers get-a-blob from a fixed SHA -> content map and
// records every SHA it was asked for.
//
// The recording is the point. What gatherBlobs returns says which files
// it understood; only the request log says which ones it *spent a call
// on*, and a budget is an argument about calls. A leak that stays
// inside the returned map would be invisible.
type blobServer struct {
	mu        sync.Mutex
	requested []string
}

func (b *blobServer) seen() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.requested...)
}

func newBlobClient(t *testing.T, content map[string]string) (*Client, *blobServer) {
	t.Helper()
	bs := &blobServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/repos/o/r/git/blobs/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		sha := strings.TrimPrefix(r.URL.Path, prefix)
		bs.mu.Lock()
		bs.requested = append(bs.requested, sha)
		bs.mu.Unlock()

		body, ok := content[sha]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(restBlob{
			Content:  base64.StdEncoding.EncodeToString([]byte(body)),
			Encoding: "base64",
			Size:     len(body),
		})
	}))
	t.Cleanup(srv.Close)
	return &Client{rest: &http.Client{Transport: &rewriteHost{host: srv.URL}}, authenticated: true}, bs
}

// hookMatch is an ordinary Axis-2 candidate: a committed git hook, one
// per SHA, small enough to be read.
func hookMatch(n int) ignitionMatch {
	return ignitionMatch{
		Path:    fmt.Sprintf(".husky/h%02d", n),
		Size:    200,
		BlobSHA: fmt.Sprintf("hook%02d", n),
		Rule:    ignitionRule{Glob: ".husky/*", Class: classVCSHook, Weight: 0},
	}
}

func lockMatch(path, sha string, size int) ignitionMatch {
	return ignitionMatch{
		Path: path, Size: size, BlobSHA: sha,
		Rule: ignitionRule{Glob: path, Class: classLockfile, Weight: 0},
	}
}

// The lockfile read must be arithmetically invisible to Axis 2. That is
// the whole justification for maxLockfileFetches existing beside
// maxBlobFetches rather than inside it, so it is asserted as a
// measurement — the same tree scanned with and without a lockfile, and
// the set of Axis-2 blobs actually pulled compared — rather than
// asserted as a comment.
func TestTheLockfileReadNeverSpendsTheAxis2Budget(t *testing.T) {
	// More hooks than the budget, so the cap really binds: if the
	// lockfile ever took a read from this pool, one hook would silently
	// stop being fetched and the two sets would differ.
	const hooks = maxBlobFetches + 2

	content := map[string]string{"lock": lockfileV3}
	var withLock, without []ignitionMatch
	for i := 0; i < hooks; i++ {
		m := hookMatch(i)
		content[m.BlobSHA] = "#!/bin/sh\nnpm test\n"
		// The lockfile sits in the middle of the tree rather than at the
		// end: a leak at the end would only drop the last hook, which a
		// sloppy assertion could miss.
		if i == 3 {
			withLock = append(withLock, lockMatch("package-lock.json", "lock", len(lockfileV3)))
		}
		withLock = append(withLock, m)
		without = append(without, m)
	}

	run := func(matches []ignitionMatch) (map[string]blobAnalysis, []string) {
		c, srv := newBlobClient(t, content)
		blobs := c.gatherBlobs(context.Background(), "o", "r",
			[]scanBranch{{Prov: provBranch("main", true), Matches: matches}})
		var axis2 []string
		for _, sha := range srv.seen() {
			if sha != "lock" {
				axis2 = append(axis2, sha)
			}
		}
		sort.Strings(axis2)
		return blobs, axis2
	}

	blobs, readWith := run(withLock)
	_, readWithout := run(without)

	if len(readWithout) != maxBlobFetches {
		t.Fatalf("without a lockfile, Axis 2 read %d blobs, want maxBlobFetches = %d "+
			"(the fixture must saturate the budget or this test measures nothing)",
			len(readWithout), maxBlobFetches)
	}
	if !reflect.DeepEqual(readWith, readWithout) {
		t.Errorf("Axis 2 read %v with a lockfile in the tree and %v without it; "+
			"the lockfile must not change which blobs the payload axis gets", readWith, readWithout)
	}

	// The other half: the lockfile was read, and its facts arrived.
	// Half a budget test — "Axis 2 is unaffected" — is also satisfied by
	// a lockfile read that silently does nothing.
	ba := blobs["lock"]
	if !ba.Fetched || ba.Lockfile == nil {
		t.Fatalf("the default branch's lockfile must be read: fetched=%v lockfile=%v", ba.Fetched, ba.Lockfile)
	}
	if !ba.Lockfile.Supported {
		t.Errorf("a v3 lockfile must come back Supported, note %q", ba.Lockfile.Note)
	}
	if _, ok := ba.Lockfile.Packages["fsevents@2.3.3"]; !ok {
		t.Errorf("install surface = %v, want the fixture's install-script packages", ba.Lockfile.Packages)
	}

	// Axis 2 must not have looked at it. A lockfile is hundreds of
	// kilobytes of base64 integrity hashes, which is exactly what
	// looksObfuscated hunts for, so these three zero values are the
	// exemption expressed in the data rather than trusted to the scorer.
	if ba.IsText || ba.Entropy != 0 || ba.Markers != nil {
		t.Errorf("Axis-2 analysis ran on a lockfile: isText=%v entropy=%v markers=%v",
			ba.IsText, ba.Entropy, ba.Markers)
	}
}

// A lockfile on a side branch is deliberately not read: on a repo with
// twenty branches it would re-report the open pull requests, once per
// branch, against a baseline recorded from the default branch. It is
// still inventoried, with its size, so the report can name it.
func TestALockfileOnASideBranchIsNotRead(t *testing.T) {
	c, srv := newBlobClient(t, map[string]string{"lock": lockfileV3})

	blobs := c.gatherBlobs(context.Background(), "o", "r", []scanBranch{
		{Prov: provBranch("main", true)},
		{Prov: provBranch("next", false), Matches: []ignitionMatch{
			lockMatch("package-lock.json", "lock", len(lockfileV3)),
		}},
	})

	if got := srv.seen(); len(got) != 0 {
		t.Errorf("requested %v, want no blob call at all for a side-branch lockfile", got)
	}
	ba := blobs["lock"]
	if ba.Fetched || ba.Lockfile != nil {
		t.Errorf("a side-branch lockfile must stay unread: fetched=%v lockfile=%v", ba.Fetched, ba.Lockfile)
	}
	if ba.Size != len(lockfileV3) {
		t.Errorf("Size = %d, want %d — the file is still inventory", ba.Size, len(lockfileV3))
	}
}

// A monorepo lockfile past maxBlobScanBytes is *declared unread*, not
// silently skipped. Size survives so the report can say why, and
// Lockfile stays nil so nothing downstream can read the absence as "no
// dependency runs code at install".
func TestAnOversizedLockfileIsDeclaredUnreadRatherThanSkipped(t *testing.T) {
	const huge = maxBlobScanBytes + 1
	c, srv := newBlobClient(t, map[string]string{"lock": lockfileV3})

	blobs := c.gatherBlobs(context.Background(), "o", "r", []scanBranch{
		{Prov: provBranch("main", true), Matches: []ignitionMatch{
			lockMatch("package-lock.json", "lock", huge),
		}},
	})

	if got := srv.seen(); len(got) != 0 {
		t.Errorf("requested %v, want no call: the size cap is checked before the fetch", got)
	}
	ba := blobs["lock"]
	if ba.Fetched || ba.Lockfile != nil {
		t.Errorf("an oversized lockfile must stay unread: fetched=%v lockfile=%v", ba.Fetched, ba.Lockfile)
	}
	if ba.Size != huge {
		t.Errorf("Size = %d, want %d — the disclosure needs the number", ba.Size, huge)
	}
}

// npm ignores package-lock.json when npm-shrinkwrap.json is present, so
// reading the wrong one would describe an install surface npm never
// uses. The budget forces the choice; this pins which way it goes, and
// that the budget is the bound.
func TestWithBothLockfilesTheOneNpmUsesIsReadFirst(t *testing.T) {
	c, srv := newBlobClient(t, map[string]string{
		"plock":  lockfileV3,
		"shrink": lockfileV3,
	})

	blobs := c.gatherBlobs(context.Background(), "o", "r", []scanBranch{
		{Prov: provBranch("main", true), Matches: []ignitionMatch{
			// Tree order puts the ignored file first, so a pass that
			// simply took what it found would take the wrong one.
			lockMatch("package-lock.json", "plock", len(lockfileV3)),
			lockMatch("npm-shrinkwrap.json", "shrink", len(lockfileV3)),
		}},
	})

	got := srv.seen()
	if len(got) != maxLockfileFetches {
		t.Fatalf("requested %v, want exactly maxLockfileFetches = %d calls", got, maxLockfileFetches)
	}
	if got[0] != "shrink" {
		t.Errorf("first lockfile read = %q, want the npm-shrinkwrap.json blob: "+
			"npm ignores package-lock.json when both are present", got[0])
	}
	if blobs["shrink"].Lockfile == nil {
		t.Error("the shrinkwrap's facts must be recorded")
	}
	if ba := blobs["plock"]; ba.Lockfile != nil {
		t.Error("the ignored lockfile must not be read")
	} else if ba.Size != len(lockfileV3) {
		t.Errorf("Size = %d, want %d — it is still inventory", ba.Size, len(lockfileV3))
	}
}

func TestLockfileReadOrderKeepsOnlyLockfilesAndIsDeterministic(t *testing.T) {
	in := []ignitionMatch{
		hookMatch(1),
		lockMatch("package-lock.json", "p", 10),
		{Path: "package.json", Size: 10, BlobSHA: "pkg",
			Rule: ignitionRule{Glob: "package.json", Class: classPackage}},
		lockMatch("npm-shrinkwrap.json", "s", 10),
		{Path: ".github/workflows/ci.yml", Size: 10, BlobSHA: "ci",
			Rule: ignitionRule{Glob: ".github/workflows/*.yml", Class: classCI}},
	}

	var got []string
	for _, m := range lockfileReadOrder(in) {
		got = append(got, m.Path)
	}
	want := []string{"npm-shrinkwrap.json", "package-lock.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("read order = %v, want %v", got, want)
	}

	// package.json is the neighbouring class this must never swallow:
	// it is Axis 2's to read, and taking it here would cost that axis a
	// blob without anyone noticing.
	if len(lockfileReadOrder([]ignitionMatch{hookMatch(1)})) != 0 {
		t.Error("a branch with no lockfile must yield nothing to read")
	}
}
