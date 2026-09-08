package github

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// depsInput builds a scan of one repository whose default branch carries
// one lockfile. Passing nil facts is "the lockfile was not read", which
// is a different input from "read and empty".
func depsInput(nowPkgs map[string]string, baseline *ScanFingerprint, now time.Time) scanInput {
	// nil packages means the repository has no lockfile at all, not one
	// that went unread — those are different inputs and step 6
	// deliberately says different things about them.
	br := scanBranch{Prov: provBranch("main", true)}
	blobs := map[string]blobAnalysis{}
	if nowPkgs != nil {
		br, blobs = lockBranch("main", true, "l1",
			&lockfileFacts{Supported: true, Packages: nowPkgs})
	}
	return scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main", BranchesTotal: 1,
		Branches: []scanBranch{br}, Blobs: blobs, Now: now, Baseline: baseline,
	}
}

func depsBaseline(capturedAt time.Time, deps map[string]map[string]string) *ScanFingerprint {
	return &ScanFingerprint{
		CapturedAt: capturedAt,
		Verdict:    VerdictClean.String(),
		Ignition:   map[string]string{},
		Signed:     map[string]bool{"main": true},
		Deps:       deps,
	}
}

func TestTheDependencyInstallSurfaceDelta(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-48 * time.Hour)
	ancient := now.Add(-120 * 24 * time.Hour)
	key := fingerprintKey("main", "package-lock.json")

	tests := []struct {
		name        string
		in          scanInput
		wantCount   int
		wantWeight  int
		wantContain string
	}{
		{
			// The headline case: something in the dependency tree began
			// executing code at install time.
			name: "a dependency that did not run code at install now does",
			in: depsInput(map[string]string{"fsevents@2.3.3": "sha512-a"},
				depsBaseline(fresh, map[string]map[string]string{key: {}}), now),
			wantCount:   1,
			wantWeight:  wDeltaNewInstallScript,
			wantContain: "runs code at install and did not at the last scan",
		},
		{
			// The sharp one. No upgrade explains it, and the measured
			// base rate is zero in 57 lockfile revisions.
			name: "the same version shipping different content",
			in: depsInput(map[string]string{"fsevents@2.3.3": "sha512-EVIL"},
				depsBaseline(fresh, map[string]map[string]string{key: {"fsevents@2.3.3": "sha512-a"}}), now),
			wantCount:   1,
			wantWeight:  wDeltaRepublishedDep,
			wantContain: "not an upgrade",
		},
		{
			// A known install-script package moving version is what
			// every dependency bot does all day. Inventory, weight 0 —
			// scoring it is how an axis becomes noise.
			name: "an ordinary bump is inventory, not a signal",
			in: depsInput(map[string]string{"fsevents@2.3.4": "sha512-b"},
				depsBaseline(fresh, map[string]map[string]string{key: {"fsevents@2.3.3": "sha512-a"}}), now),
			wantCount:   1,
			wantWeight:  0,
			wantContain: "already ran code at install",
		},
		{
			name: "a dependency that stopped running code at install",
			in: depsInput(map[string]string{},
				depsBaseline(fresh, map[string]map[string]string{key: {"fsevents@2.3.3": "sha512-a"}}), now),
			wantCount:   1,
			wantWeight:  0,
			wantContain: "no longer runs code at install",
		},
		{
			name: "an unchanged surface says nothing",
			in: depsInput(map[string]string{"fsevents@2.3.3": "sha512-a"},
				depsBaseline(fresh, map[string]map[string]string{key: {"fsevents@2.3.3": "sha512-a"}}), now),
			wantCount:  0,
			wantWeight: 0,
		},
		{
			// A baseline that predates the axis. The repo has a history,
			// this axis does not, and saying nothing would read as "your
			// dependency surface did not change".
			name: "a baseline recorded before this axis existed",
			in: depsInput(map[string]string{"fsevents@2.3.3": "sha512-a"},
				depsBaseline(fresh, nil), now),
			wantCount:   1,
			wantWeight:  0,
			wantContain: "first comparison of the dependency install surface",
		},
		{
			// ...but only when there was something to compare. A repo
			// with no lockfile must not carry that line on every scan
			// forever; that is how a reader learns to skip an axis.
			name:       "nothing measured on either side says nothing",
			in:         depsInput(nil, depsBaseline(fresh, nil), now),
			wantCount:  0,
			wantWeight: 0,
		},
		{
			// The staleness gate is shared with every other delta case:
			// past the window the finding is still reported, and still
			// carries no weight.
			name: "a stale baseline reports without scoring",
			in: depsInput(map[string]string{"fsevents@2.3.3": "sha512-EVIL"},
				depsBaseline(ancient, map[string]map[string]string{key: {"fsevents@2.3.3": "sha512-a"}}), now),
			wantCount:   1,
			wantWeight:  0,
			wantContain: "without affecting the verdict",
		},
		{
			// A lockfile read now and not last time. Diffing it against
			// nothing would report every install-script dependency in it
			// as newly arrived.
			name: "a path compared for the first time",
			in: depsInput(map[string]string{"fsevents@2.3.3": "sha512-a"},
				depsBaseline(fresh, map[string]map[string]string{
					fingerprintKey("main", "npm-shrinkwrap.json"): {},
				}), now),
			wantCount:   2, // the first comparison, plus the vanished shrinkwrap
			wantWeight:  0,
			wantContain: "was not compared at the last scan",
		},
		{
			// Nothing else notices a lockfile going unread: it is weight
			// 0, so its path is not in Ignition and its disappearance
			// leaves no trace there.
			name: "a surface recorded before and not measured now",
			in: depsInput(nil,
				depsBaseline(fresh, map[string]map[string]string{key: {"fsevents@2.3.3": "sha512-a"}}), now),
			wantCount:   1,
			wantWeight:  0,
			wantContain: "was not compared this time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deltaFindings(evaluateScan(tt.in))
			if len(got) != tt.wantCount {
				t.Fatalf("delta findings = %d, want %d: %+v", len(got), tt.wantCount, got)
			}
			weight := 0
			joined := ""
			for _, f := range got {
				weight += f.Weight
				joined += f.Reason + "\n"
			}
			if weight != tt.wantWeight {
				t.Errorf("delta weight = %d, want %d (%s)", weight, tt.wantWeight, joined)
			}
			if tt.wantContain != "" && !strings.Contains(joined, tt.wantContain) {
				t.Errorf("no finding mentions %q; got:\n%s", tt.wantContain, joined)
			}
		})
	}
}

// A scoped name carries an "@" of its own, so splitting the key on the
// first one would name the package "" and make every scoped dependency
// look like the same package at different versions.
func TestSplitDepKeyHandlesAScopedName(t *testing.T) {
	tests := map[string][2]string{
		"fsevents@2.3.3":     {"fsevents", "2.3.3"},
		"@scope/pkg@1.0.0":   {"@scope/pkg", "1.0.0"},
		"packages/cli@0.4.0": {"packages/cli", "0.4.0"},
		"noversion@":         {"noversion", ""},
	}
	for key, want := range tests {
		name, version := splitDepKey(key)
		if name != want[0] || version != want[1] {
			t.Errorf("splitDepKey(%q) = (%q, %q), want (%q, %q)", key, name, version, want[0], want[1])
		}
	}
}

// A scoped package gaining an install script must read as one arrival,
// not as an arrival plus a departure — which is what a first-"@" split
// produces, since every scoped name reduces to "".
func TestAScopedDependencyIsTrackedByItsRealName(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	key := fingerprintKey("main", "package-lock.json")

	got := deltaFindings(evaluateScan(depsInput(
		map[string]string{"@scope/tool@2.0.0": "sha512-b"},
		depsBaseline(now.Add(-48*time.Hour), map[string]map[string]string{
			key: {"@scope/tool@1.0.0": "sha512-a"},
		}), now)))

	if len(got) != 1 {
		t.Fatalf("a scoped bump must be one finding, got %d: %+v", len(got), got)
	}
	if got[0].Weight != 0 {
		t.Errorf("a bump of a known install-script package must not score, got %d: %s", got[0].Weight, got[0].Reason)
	}
	if !strings.Contains(got[0].Reason, "@scope/tool") {
		t.Errorf("the finding must name the scoped package, got %q", got[0].Reason)
	}
}

// The lockfile is attacker-controlled and bounded only by
// maxBlobScanBytes, which is tens of thousands of minimal entries — more
// than enough to bury every other axis under this one's output.
func TestOneLockfileCannotFloodTheReport(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	key := fingerprintKey("main", "package-lock.json")

	pkgs := map[string]string{}
	for i := 0; i < maxDepFindingsPerPath*4; i++ {
		pkgs[fmt.Sprintf("pkg%03d@1.0.0", i)] = "sha512-a"
	}
	got := deltaFindings(evaluateScan(depsInput(pkgs,
		depsBaseline(now.Add(-48*time.Hour), map[string]map[string]string{key: {}}), now)))

	// The cap, plus the one line that states how many were left out.
	if len(got) != maxDepFindingsPerPath+1 {
		t.Fatalf("findings = %d, want %d capped plus one summary", len(got), maxDepFindingsPerPath+1)
	}
	last := got[len(got)-1]
	if last.Weight != 0 || !strings.Contains(last.Reason, "not listed") {
		t.Errorf("the last finding must be the weight-0 remainder line, got %+v", last)
	}
	// Silence about the remainder would be the real failure: a report
	// that shows 25 of 100 changes and does not say so is worse than one
	// that shows none.
	if !strings.Contains(last.Reason, fmt.Sprintf("%d further", maxDepFindingsPerPath*3)) {
		t.Errorf("the remainder line must state how many were left out, got %q", last.Reason)
	}
}

// Go randomises map iteration and the delta walks three maps. A report
// whose findings shuffle between two runs of the same scan cannot be
// diffed by eye, and the shuffle would land hardest on exactly the
// repositories with the most to read.
func TestTheDependencyDeltaIsBytewiseDeterministic(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	key := fingerprintKey("main", "package-lock.json")

	in := func() scanInput {
		return depsInput(
			map[string]string{
				"alpha@1.0.0": "sha512-NEW", // republished
				"beta@2.0.0":  "sha512-b",   // bumped
				"gamma@1.0.0": "sha512-c",   // arrived
				"delta@1.0.0": "sha512-d",   // arrived
			},
			depsBaseline(now.Add(-48*time.Hour), map[string]map[string]string{
				key: {
					"alpha@1.0.0":   "sha512-a",
					"beta@1.0.0":    "sha512-b0",
					"epsilon@1.0.0": "sha512-e", // departed
				},
			}), now)
	}

	first := deltaFindings(evaluateScan(in()))
	if len(first) < 5 {
		t.Fatalf("fixture produced %d findings; it must exercise every case", len(first))
	}
	for i := 0; i < 30; i++ {
		if got := deltaFindings(evaluateScan(in())); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d disagrees:\n got %+v\nwant %+v", i, got, first)
		}
	}
}
