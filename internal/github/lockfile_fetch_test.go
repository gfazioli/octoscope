package github

import (
	"context"
	"errors"
	"fmt"
	"io"
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
		// Raw, as the blobs API answers `Accept: application/vnd.github.raw`
		// since #167 — the file itself, not base64 inside an envelope.
		w.Header().Set("Content-Type", "application/vnd.github.raw")
		_, _ = io.WriteString(w, body)
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
	if ba.LockfileUnread != lockUnreadSideBranch {
		t.Errorf("unread reason = %q, want %q", ba.LockfileUnread, lockUnreadSideBranch)
	}
}

// A monorepo lockfile past maxLockfileScanBytes is *declared unread*, not
// silently skipped. Size survives so the report can say why, and
// Lockfile stays nil so nothing downstream can read the absence as "no
// dependency runs code at install".
func TestAnOversizedLockfileIsDeclaredUnreadRatherThanSkipped(t *testing.T) {
	const huge = maxLockfileScanBytes + 1
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
	if ba.LockfileUnread != lockUnreadOversized {
		t.Errorf("unread reason = %q, want %q", ba.LockfileUnread, lockUnreadOversized)
	}
}

// The behaviour #159 asked for: a lockfile larger than Axis 2's blob cap
// is now read, because the lockfile read has a ceiling of its own. The
// two repositories that sat in this gap at the time of measuring —
// WordPress/gutenberg at 1.81 MiB and puppeteer/puppeteer at 1.63 — are
// the shape the axis is worth most on, and they were the ones it could
// not see.
func TestALockfileAboveTheBlobCapIsStillRead(t *testing.T) {
	size := maxBlobScanBytes + 1
	if size > maxLockfileScanBytes {
		t.Fatalf("fixture size %d is past the lockfile cap; this test would prove the opposite", size)
	}
	c, srv := newBlobClient(t, map[string]string{"lock": lockfileV3})

	blobs := c.gatherBlobs(context.Background(), "o", "r", []scanBranch{
		{Prov: provBranch("main", true), Matches: []ignitionMatch{
			lockMatch("package-lock.json", "lock", size),
		}},
	})

	if got := srv.seen(); len(got) != 1 {
		t.Errorf("requested %v, want exactly one fetch: this size is inside the lockfile budget", got)
	}
	ba := blobs["lock"]
	if ba.LockfileUnread != "" {
		t.Errorf("unread reason = %q, want none — %d bytes is under the %d-byte lockfile cap",
			ba.LockfileUnread, size, maxLockfileScanBytes)
	}
	if ba.Lockfile == nil || len(ba.Lockfile.Packages) == 0 {
		t.Errorf("lockfile = %v, want the install surface parsed", ba.Lockfile)
	}
}

// The cap is an argument about memory, so it is enforced where the memory
// is actually allocated: on the READ. The tree entry decided whether to
// ask at all — it costs no request — but a server that sends more than it
// advertised would otherwise be allocated in full before anyone noticed.
// Since #167 the body arrives raw, so there is no self-reported size to
// trust and the bytes themselves are the bound.
func TestABlobLargerThanItsTreeEntryClaimsIsNotRead(t *testing.T) {
	// The tree entry advertised a small file; the server sends one byte
	// past the cap.
	oversized := strings.Repeat("x", maxLockfileScanBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, oversized)
	}))
	t.Cleanup(srv.Close)

	c := &Client{rest: &http.Client{Transport: &rewriteHost{base: http.DefaultTransport, host: srv.URL}}}
	got, err := c.fetchBlob(context.Background(), "o", "r", "sha", maxLockfileScanBytes)
	if got != nil {
		t.Errorf("read %d bytes, want none — the body is %d, past the %d-byte cap",
			len(got), len(oversized), maxLockfileScanBytes)
	}
	// An error, not a quiet nil. Both callers read (nil, nil) as a
	// successful fetch of an empty file, which turns "we did not read it"
	// into a claim about its contents — the lockfile branch disclosing a
	// parse failure, and Axis 2 recording zero entropy and no markers,
	// which can only remove findings.
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want a *FetchError so the callers take their did-not-arrive paths", err)
	}
	if fe.Reason != ReasonServer {
		t.Errorf("reason = %v, want ReasonServer: the blob outgrew what the tree entry cleared", fe.Reason)
	}
}

// The previous test proves the OUTCOME — an over-limit blob errors — and
// a mutation showed it proves nothing about the bound: remove the
// io.LimitReader and it still passes, because the length check after
// ReadAll catches the same case. But by then the whole body has been
// allocated, which is the one thing the cap exists to prevent.
//
// So this one counts what was actually read. A body of 10 MiB behind a
// 64-byte cap must yield at most 65 bytes off the wire: the cap, plus the
// one byte that tells "exactly at the limit" from "over it".
type countingBody struct {
	data []byte
	read int
}

func (c *countingBody) Read(p []byte) (int, error) {
	if c.read >= len(c.data) {
		return 0, io.EOF
	}
	n := copy(p, c.data[c.read:])
	c.read += n
	return n, nil
}

func (c *countingBody) Close() error { return nil }

type bodyTransport struct{ body *countingBody }

func (t *bodyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: t.body, Header: http.Header{}}, nil
}

func TestAnOverLimitBlobIsNotReadPastTheCap(t *testing.T) {
	const limit = 64
	body := &countingBody{data: []byte(strings.Repeat("z", 10<<20))}

	c := &Client{rest: &http.Client{Transport: &bodyTransport{body: body}}}
	if _, err := c.fetchBlob(context.Background(), "o", "r", "sha", limit); err == nil {
		t.Fatal("fetchBlob returned no error for a body far past the cap")
	}
	if body.read > limit+1 {
		t.Errorf("read %d bytes of a %d-byte body; the cap is %d, so at most %d may be read — "+
			"without the bound the whole file is allocated before being rejected",
			body.read, len(body.data), limit, limit+1)
	}
}

// A file exactly AT the cap is not over it, and the one extra byte the
// reader takes is what tells the two apart.
func TestABlobExactlyAtTheCapIsRead(t *testing.T) {
	const limit = 64
	body := strings.Repeat("y", limit)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := &Client{rest: &http.Client{Transport: &rewriteHost{base: http.DefaultTransport, host: srv.URL}}}
	got, err := c.fetchBlob(context.Background(), "o", "r", "sha", limit)
	if err != nil {
		t.Fatalf("fetchBlob: %v", err)
	}
	if len(got) != limit {
		t.Errorf("read %d bytes, want %d — a file at the cap is inside it", len(got), limit)
	}
}

// The same response, through the caller that matters: the report must say
// the content could not be fetched, never that it could not be parsed.
func TestAnOverLimitLockfileBlobIsDisclosedAsUnfetched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxLockfileScanBytes+1))
	}))
	t.Cleanup(srv.Close)
	c := &Client{rest: &http.Client{Transport: &rewriteHost{host: srv.URL}}, authenticated: true}

	blobs := c.gatherBlobs(context.Background(), "o", "r", []scanBranch{
		{Prov: provBranch("main", true), Matches: []ignitionMatch{
			lockMatch("package-lock.json", "lock", len(lockfileV3)),
		}},
	})

	ba := blobs["lock"]
	if ba.LockfileUnread != lockUnreadFetchFailed {
		t.Errorf("unread reason = %q, want %q", ba.LockfileUnread, lockUnreadFetchFailed)
	}
	if ba.Fetched || ba.Lockfile != nil {
		t.Errorf("fetched=%v lockfile=%v — nothing was read, and the report must not imply otherwise", ba.Fetched, ba.Lockfile)
	}
}

// The two caps answer different questions and must not be collapsed back
// into one: maxBlobScanBytes bounds what Axis 2 hands to entropy and
// obfuscation analysis, while this one bounds a JSON parse whose cost was
// measured. A future edit that equalises them would silently re-open #159,
// and every other test here would stay green.
func TestTheLockfileCapIsItsOwnBudget(t *testing.T) {
	if maxLockfileScanBytes <= maxBlobScanBytes {
		t.Fatalf("lockfile cap %d must exceed the blob cap %d, or the lockfile read is back on Axis 2's budget",
			maxLockfileScanBytes, maxBlobScanBytes)
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
	} else if ba.LockfileUnread != lockUnreadNotAuthoritative {
		t.Errorf("unread reason = %q, want %q", ba.LockfileUnread, lockUnreadNotAuthoritative)
	} else if ba.Size != len(lockfileV3) {
		t.Errorf("Size = %d, want %d — it is still inventory", ba.Size, len(lockfileV3))
	}
}

// The precedence claim has to hold when the authoritative file is
// UNREADABLE, which is where the first version of this broke: the budget
// counted successful reads, so an oversized or unfetchable
// npm-shrinkwrap.json left it unspent and the loop fell through to the
// package-lock.json npm ignores — then reported a delta about the wrong
// file, under a comment still claiming precedence. Falling back answers
// the question about a file npm never installs from; not answering it is
// the honest outcome.
func TestAnUnreadableShrinkwrapDoesNotFallBackToTheFileNpmIgnores(t *testing.T) {
	cases := []struct {
		name       string
		shrinkSize int
		serve      map[string]string // what the blob server knows
		wantReason lockfileUnread
		wantCalls  int
	}{
		{
			name:       "oversized",
			shrinkSize: maxLockfileScanBytes + 1,
			serve:      map[string]string{"plock": lockfileV3, "shrink": lockfileV3},
			wantReason: lockUnreadOversized,
			wantCalls:  0, // the cap is checked before the request
		},
		{
			name:       "fetch fails",
			shrinkSize: len(lockfileV3),
			serve:      map[string]string{"plock": lockfileV3}, // "shrink" 404s
			wantReason: lockUnreadFetchFailed,
			wantCalls:  1, // tried once, and not retried against the other file
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, srv := newBlobClient(t, tc.serve)
			blobs := c.gatherBlobs(context.Background(), "o", "r", []scanBranch{
				{Prov: provBranch("main", true), Matches: []ignitionMatch{
					lockMatch("package-lock.json", "plock", len(lockfileV3)),
					lockMatch("npm-shrinkwrap.json", "shrink", tc.shrinkSize),
				}},
			})

			if got := srv.seen(); len(got) != tc.wantCalls {
				t.Errorf("requested %v, want %d call(s)", got, tc.wantCalls)
			}
			if ba := blobs["plock"]; ba.Lockfile != nil {
				t.Errorf("the ignored lockfile was read anyway: %v", ba.Lockfile.Packages)
			} else if ba.LockfileUnread != lockUnreadNotAuthoritative {
				t.Errorf("package-lock reason = %q, want %q", ba.LockfileUnread, lockUnreadNotAuthoritative)
			}
			if ba := blobs["shrink"]; ba.LockfileUnread != tc.wantReason {
				t.Errorf("shrinkwrap reason = %q, want %q", ba.LockfileUnread, tc.wantReason)
			}
		})
	}
}

// Two paths with identical content share one git blob SHA. The lockfile
// pass used to mark that SHA in the dedupe map Axis 2 reads, so any
// other ignition file with the same content was skipped — and since the
// lockfile pass deliberately fills none of the Axis-2 fields, the skip
// left nothing behind to notice. Contrived content, but the blob SHA is
// the scan's identity for a file and a hole in it is not contrived.
func TestALockfileNeverSuppressesAxis2AnalysisOfTheSameContent(t *testing.T) {
	const sha = "shared"
	c, srv := newBlobClient(t, map[string]string{sha: lockfileV3})

	blobs := c.gatherBlobs(context.Background(), "o", "r", []scanBranch{
		{Prov: provBranch("main", true), Matches: []ignitionMatch{
			lockMatch("package-lock.json", sha, len(lockfileV3)),
			{Path: ".claude/settings.json", Size: len(lockfileV3), BlobSHA: sha,
				Rule: ignitionRule{Glob: ".claude/settings.json", Class: classAgentHook, Weight: wIgnitionAgentHook}},
		}},
	})

	ba := blobs[sha]
	if ba.Lockfile == nil {
		t.Error("the lockfile facts must survive the Axis-2 analysis of the same blob")
	}
	// Entropy is filled only by the Axis-2 pass, so a non-zero value is
	// proof that pass ran rather than an inference from Fetched, which
	// both passes set.
	if ba.Entropy == 0 || !ba.IsText {
		t.Errorf("Axis 2 never analysed the shared blob: entropy=%v isText=%v", ba.Entropy, ba.IsText)
	}
	// One read per analysis. Caching the content across the two passes
	// would save this, and is not worth the coupling for a case this
	// rare — but it should not silently become three.
	if got := srv.seen(); len(got) != 2 {
		t.Errorf("requested %v, want one read per pass", got)
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
