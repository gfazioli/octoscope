package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/shurcooL/githubv4"
)

// This file implements octoscope's supply-chain integrity scan — a
// generic detector for the Shai-Hulud / Miasma class of attack
// (self-replicating implant pushed to repos you own via a stolen
// token, auto-executing when the repo is opened in an AI editor or
// installed, then harvesting credentials). See the design section in
// ROADMAP.md for the full rationale.
//
// The detector deliberately does NOT match a single payload filename
// (that signature rots the moment the worm renames its dropper).
// Instead it scores the *invariant* of the attack across four
// filename-agnostic axes:
//
//	Axis 1 — auto-execution surface ("ignition points"): a
//	         data-driven catalog of path globs that run by themselves
//	         when a repo is opened / built (ignitionCatalog).
//	Axis 2 — blob anomaly: a matched ignition file that is oversized,
//	         high-entropy, or carries obfuscation markers — regardless
//	         of its name (blobAnalysis / looksObfuscated).
//	Axis 3 — provenance anomaly: branch tips that are unsigned against
//	         an otherwise-signed history, or forged under a bot /
//	         maintainer identity (BranchProvenance scoring).
//	Axis 4 — capability escalation (self-hosted runners, deploy keys,
//	         contents:write workflows) is intentionally left for a
//	         follow-up; the ignition catalog already lists workflow
//	         files so they show in the inventory.
//
// Scoring is weighted and explainable: every Finding carries the
// human reason it fired, and no single axis is enough to reach the
// high verdict tiers — the combination is what escalates. octoscope
// is read-only: the scan flags and explains, it never mutates the
// repository.

// ---------------------------------------------------------------------------
// Tier A — push-burst heuristic (pure, no network)
// ---------------------------------------------------------------------------

// pushBurstMinRepos / pushBurstWindow define the push-burst heuristic
// (Axis 3, account-wide). The reference worm pushed to five repos in a
// 49-second window from a single IP — humans almost never push to
// three *distinct* repos inside a two-minute window, but a fan-out
// script does. Kept tight so the dashboard banner stays a rare,
// meaningful signal rather than firing on a normal cross-repo work
// session.
const (
	pushBurstMinRepos = 3
	pushBurstWindow   = 2 * time.Minute
)

// PushBurst describes the tightest cluster of repositories pushed
// within pushBurstWindow of one another. It's the cheapest signal of
// the worm's "re-push myself to every repo I can reach" behaviour:
// pure arithmetic over the PushedAt timestamps octoscope already holds
// after a dashboard refresh, so it costs zero extra API calls.
type PushBurst struct {
	Count  int           // repos in the cluster (>= pushBurstMinRepos when detected)
	Span   time.Duration // wall-clock span between the oldest and newest push in the cluster
	Newest time.Time     // most recent push in the cluster
	Repos  []string      // repo names in the cluster, newest push first
}

// DetectPushBurst returns the tightest cluster of >= minRepos repos
// whose PushedAt timestamps all fall within `window` of the cluster's
// newest push, and reports whether such a cluster exists. Repos with a
// zero PushedAt (never pushed) are ignored.
//
// Pure function — the caller passes the thresholds so the heuristic is
// table-testable.
//
// NOT currently wired into the UI. It shipped as an always-on dashboard
// banner but was pulled: timing alone can't tell a worm's fan-out from
// an ordinary batch push (a maintainer scripting an update across N
// repos, Dependabot, a CI sweep), and — critically — this finds *any*
// historical cluster, so without a recency gate a months-old batch
// re-alarms on every launch. Retained as a tested building block: a
// future use should fold it into the on-demand scan as one input
// (e.g. "this repo was part of a push burst in the last hour"),
// gated on recency and combined with the stronger Axis 1-3 signals
// rather than standing alone.
func DetectPushBurst(repos []Repo, minRepos int, window time.Duration) (PushBurst, bool) {
	pushed := make([]Repo, 0, len(repos))
	for _, r := range repos {
		if !r.PushedAt.IsZero() {
			pushed = append(pushed, r)
		}
	}
	if len(pushed) < minRepos {
		return PushBurst{}, false
	}
	// Sort newest-first so a window anchored on entry i covers every
	// later entry pushed within `window` before it.
	sort.Slice(pushed, func(i, j int) bool {
		return pushed[i].PushedAt.After(pushed[j].PushedAt)
	})

	var best PushBurst
	for i := range pushed {
		anchor := pushed[i].PushedAt
		cluster := []string{pushed[i].Name}
		oldest := anchor
		for j := i + 1; j < len(pushed); j++ {
			if anchor.Sub(pushed[j].PushedAt) <= window {
				cluster = append(cluster, pushed[j].Name)
				oldest = pushed[j].PushedAt
			} else {
				// Sorted desc: once one falls outside the window every
				// later one does too.
				break
			}
		}
		if len(cluster) > best.Count {
			best = PushBurst{
				Count:  len(cluster),
				Span:   anchor.Sub(oldest),
				Newest: anchor,
				Repos:  cluster,
			}
		}
	}
	if best.Count < minRepos {
		return PushBurst{}, false
	}
	return best, true
}

// ---------------------------------------------------------------------------
// Axis 1 — ignition catalog
// ---------------------------------------------------------------------------

// ignitionClass groups auto-execution surfaces by how they fire, so
// the report can explain *why* a path is dangerous rather than just
// listing it.
type ignitionClass string

const (
	classAgentHook  ignitionClass = "AI-agent / editor session hook"
	classAgentInstr ignitionClass = "AI-agent instruction / rules file"
	classEditorTask ignitionClass = "editor auto-run task"
	classDevcontain ignitionClass = "devcontainer lifecycle hook"
	classPackage    ignitionClass = "package lifecycle script"
	classVCSHook    ignitionClass = "committed VCS hook"
	classCI         ignitionClass = "CI workflow"
	classDropper    ignitionClass = "known dropper filename"
)

// ignitionRule is one entry in the data-driven ignition catalog. Glob
// is matched against repo-root-relative tree paths via path.Match
// (so `*` never crosses a `/`). Weight is the *base* score a bare
// match contributes — deliberately low (or zero) for ubiquitous,
// usually-legitimate surfaces, because Axis 2/3 are what escalate.
// Adding a future ignition location is a one-line edit here, not a
// code change — this is the generic-detection contract from the
// ROADMAP design.
type ignitionRule struct {
	Glob   string
	Class  ignitionClass
	Weight int
	Note   string
}

// ignitionCatalog is the seed list of auto-execution surfaces. The
// weights encode "how surprising is this in an established repo":
//
//   - wIgnitionNamedIOC for the literal Shai-Hulud dropper name (a
//     strong, specific seed — but still just one row).
//   - wIgnitionAgentHook for AI-agent / editor hooks that can auto-run
//     a command or process (Claude/Gemini session hooks, MCP, Aider,
//     Continue): rarer than package manifests, and the code-execution
//     vector in the reference incident.
//   - 0 for ubiquitous or prompt-only surfaces (package.json, .vscode,
//     workflows, husky, AND instruction/rules files like
//     copilot-instructions.md / .cursor/rules / .windsurfrules /
//     AGENTS.md): listed in the inventory so the user sees their
//     attack surface, but they don't inflate the score on their own —
//     otherwise every modern repo would cry "watch". They escalate
//     only when Axis 2 (oversized / obfuscated blob) fires on them.
var ignitionCatalog = []ignitionRule{
	// Known dropper filename — the 2026-06 reference IOC. One row,
	// high weight; the rest of the engine is name-agnostic.
	{Glob: ".github/setup.js", Class: classDropper, Weight: wIgnitionNamedIOC, Note: "matches the reference Miasma / Shai-Hulud dropper filename"},

	// AI-agent / editor session hooks that can auto-run a shell command
	// or launch a process on repo open — the real code-execution
	// vector. Weight 1 (a mild heads-up; Axis 2/3 escalate).
	{Glob: ".claude/settings.json", Class: classAgentHook, Weight: wIgnitionAgentHook, Note: "Claude session hooks run on SessionStart"},
	{Glob: ".claude/settings.local.json", Class: classAgentHook, Weight: wIgnitionAgentHook, Note: "Claude session hooks run on SessionStart"},
	{Glob: ".gemini/settings.json", Class: classAgentHook, Weight: wIgnitionAgentHook, Note: "Gemini session hooks run on open"},
	{Glob: ".aider.conf.yml", Class: classAgentHook, Weight: wIgnitionAgentHook, Note: "Aider config may run commands"},
	{Glob: ".mcp.json", Class: classAgentHook, Weight: wIgnitionAgentHook, Note: "MCP server config — may launch processes"},
	{Glob: ".vscode/mcp.json", Class: classAgentHook, Weight: wIgnitionAgentHook, Note: "MCP server config — may launch processes"},
	{Glob: ".continue/*.json", Class: classAgentHook, Weight: wIgnitionAgentHook, Note: "Continue config may run commands"},

	// AI-agent instruction / rules files — prompt-injection surface,
	// NOT direct code execution, and increasingly ubiquitous (GitHub
	// promotes copilot-instructions.md). Weight 0: listed in the
	// inventory so the user sees the surface, but they never drive the
	// verdict on their own — that's what kept the real (now-cleaned)
	// victim repos out of a spurious "watch".
	{Glob: ".cursor/rules/*.mdc", Class: classAgentInstr, Weight: 0, Note: "Cursor rules auto-applied into agent context"},
	{Glob: ".github/copilot-instructions.md", Class: classAgentInstr, Weight: 0, Note: "Copilot instructions auto-loaded into context"},
	{Glob: ".windsurfrules", Class: classAgentInstr, Weight: 0, Note: "Windsurf rules auto-loaded into context"},
	{Glob: "AGENTS.md", Class: classAgentInstr, Weight: 0, Note: "agent instructions auto-loaded into context"},

	// Editor auto-run.
	{Glob: ".vscode/tasks.json", Class: classEditorTask, Weight: 0, Note: "may carry runOn: folderOpen"},
	{Glob: ".vscode/settings.json", Class: classEditorTask, Weight: 0, Note: "editor settings"},
	{Glob: ".vscode/launch.json", Class: classEditorTask, Weight: 0, Note: "debug launch config"},

	// Devcontainer lifecycle (postCreate / onCreate / postStart).
	{Glob: ".devcontainer/devcontainer.json", Class: classDevcontain, Weight: 0, Note: "lifecycle commands run on container build"},
	{Glob: ".devcontainer/*/devcontainer.json", Class: classDevcontain, Weight: 0, Note: "lifecycle commands run on container build"},

	// Package lifecycle scripts (preinstall / postinstall / prepare).
	{Glob: "package.json", Class: classPackage, Weight: 0, Note: "inspect lifecycle scripts (pre/post-install, prepare)"},

	// Committed VCS hooks.
	{Glob: ".husky/*", Class: classVCSHook, Weight: 0, Note: "git hook runs on commit / install"},

	// CI workflows — the home of capability escalation (Axis 4).
	{Glob: ".github/workflows/*.yml", Class: classCI, Weight: 0, Note: "runs in CI; inspect permissions / triggers"},
	{Glob: ".github/workflows/*.yaml", Class: classCI, Weight: 0, Note: "runs in CI; inspect permissions / triggers"},
}

// matchIgnition returns the first catalog rule whose glob matches the
// given repo-root-relative path. The boolean is false when nothing
// matches. path.Match treats `*` as not crossing `/`, which is exactly
// per-segment matching — so ".github/workflows/*.yml" matches
// ".github/workflows/ci.yml" but not a nested path.
func matchIgnition(p string) (ignitionRule, bool) {
	for _, rule := range ignitionCatalog {
		if ok, _ := path.Match(rule.Glob, p); ok {
			return rule, true
		}
	}
	return ignitionRule{}, false
}

// ---------------------------------------------------------------------------
// Scoring weights & verdict thresholds
// ---------------------------------------------------------------------------

const (
	// Axis 1 base weights.
	wIgnitionAgentHook      = 1
	wIgnitionNamedIOC       = 4
	wIgnitionNonDefaultOnly = 2 // ignition file on a side branch but not the default branch (divergence)

	// Axis 2 — blob anomaly.
	wBlobOversized    = 4
	wBlobObfuscated   = 5
	oversizeThreshold = 256 * 1024 // 256 KiB: absurd for a config / hook file (reference dropper was 4.3 MB)

	// Axis 3 — provenance anomaly.
	wProvUnsignedDelta = 3 // unsigned tip on a branch carrying an ignition file, in a repo that otherwise signs
	wProvSpoofIdentity = 5 // tip forged under a bot / GitHub-Actions identity but not signed by GitHub

	// Cross-axis bonus — the smoking gun: ignition + blob anomaly +
	// provenance anomaly all on the same branch.
	wCombinedSmokingGun = 4
)

// Verdict thresholds. A lone agent-hook config (weight 1) stays Clean
// but listed in the inventory; multiple hooks reach Watch; an
// oversized/obfuscated dropper with a forged tip lands in Compromised.
const (
	tWatch       = 2
	tSuspicious  = 5
	tCompromised = 10
)

// ---------------------------------------------------------------------------
// Result model
// ---------------------------------------------------------------------------

// ScanVerdict is the severity tier the engine assigns to a repo.
type ScanVerdict int

const (
	VerdictClean ScanVerdict = iota
	VerdictWatch
	VerdictSuspicious
	VerdictCompromised
)

// String renders the verdict as a short label for the UI / logs.
func (v ScanVerdict) String() string {
	switch v {
	case VerdictClean:
		return "clean"
	case VerdictWatch:
		return "watch"
	case VerdictSuspicious:
		return "suspicious"
	case VerdictCompromised:
		return "likely compromised"
	default:
		return "unknown"
	}
}

// verdictFor maps a total score to its tier.
func verdictFor(score int) ScanVerdict {
	switch {
	case score >= tCompromised:
		return VerdictCompromised
	case score >= tSuspicious:
		return VerdictSuspicious
	case score >= tWatch:
		return VerdictWatch
	default:
		return VerdictClean
	}
}

// FindingAxis names which detection axis produced a Finding, so the
// report can group evidence and the user can reason about it.
type FindingAxis string

const (
	AxisIgnition   FindingAxis = "ignition"
	AxisBlob       FindingAxis = "blob"
	AxisProvenance FindingAxis = "provenance"
)

// Finding is one piece of explainable evidence. Weight is the score it
// contributed (0 for inventory-only entries). Reason is a
// human-readable, already-sanitized sentence — the user audits the
// evidence, never a black-box number.
type Finding struct {
	Axis   FindingAxis
	Branch string // "" when repo-wide
	Path   string // "" when not file-specific
	Weight int
	Reason string
}

// BranchProvenance carries the tip-commit facts the engine reasons
// about for Axis 3. Identity fields fall back to the raw git name when
// GitHub can't link the commit to a user account.
type BranchProvenance struct {
	Name      string
	IsDefault bool

	TipOID      string
	TipHeadline string
	CommittedAt time.Time

	AuthorName    string
	AuthorLogin   string
	CommitterName string

	// Signed is true when the tip carries a valid signature; Bot is
	// true when the author/committer identity looks like the
	// GitHub-Actions bot; SignedByGitHub distinguishes a *genuine*
	// Actions commit (GitHub-signed) from a forgery that merely wears
	// the bot's name.
	SignatureState string
	Signed         bool
	Bot            bool
	SignedByGitHub bool
}

// RepoScan is the full, UI-facing result of FetchRepoScan.
type RepoScan struct {
	Owner string
	Name  string
	URL   string

	DefaultBranch   string
	BranchesScanned int
	BranchesTotal   int
	Truncated       bool // some branches / trees were not fully walked (bounded fan-out)

	Findings []Finding
	Branches []BranchProvenance

	Score   int
	Verdict ScanVerdict
}

// IgnitionInventory returns the auto-execution surface (Axis 1),
// de-duplicated to one entry per path — the report always lists this,
// even on a Clean verdict, so the user sees what auto-executes in the
// repo regardless of detection. The base ignition finding for a path
// is emitted before any divergence finding for the same path, so the
// first-seen entry is the surface description rather than the anomaly.
func (s *RepoScan) IgnitionInventory() []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, f := range s.Findings {
		if f.Axis != AxisIgnition || f.Path == "" || seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		out = append(out, f)
	}
	return out
}

// ScoredFindings returns the findings that contributed to the score
// (Weight > 0), i.e. the actual evidence behind the verdict.
func (s *RepoScan) ScoredFindings() []Finding {
	var out []Finding
	for _, f := range s.Findings {
		if f.Weight > 0 {
			out = append(out, f)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Axis 2 — blob heuristics (pure)
// ---------------------------------------------------------------------------

// blobAnalysis holds the Axis-2 verdict for one matched ignition blob,
// keyed by its git blob SHA so identical content shared across
// branches is fetched and analysed once.
type blobAnalysis struct {
	Size    int
	Fetched bool // content was actually pulled (within the size cap)
	IsText  bool
	Entropy float64
	Markers []string // human-readable obfuscation markers, already plain ASCII
}

// maxBlobScanBytes caps how large a matched ignition blob we'll pull
// for content analysis. Above it we still flag "oversized" (a strong
// signal on its own) but skip entropy/marker inspection — no point
// downloading a multi-megabyte dropper to confirm what its size
// already told us.
const maxBlobScanBytes = 1536 * 1024 // 1.5 MiB

// shannonEntropy returns the Shannon entropy of b in bits per byte
// (0..8). Minified / encrypted / packed payloads sit high (> ~5);
// ordinary source and config sit lower.
func shannonEntropy(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	var counts [256]int
	for _, c := range b {
		counts[c]++
	}
	n := float64(len(b))
	var h float64
	for _, c := range counts {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// looksObfuscated inspects content (a matched ignition file's bytes)
// for the filename-agnostic tells of a packed / encoded payload, and
// returns the human-readable markers it found. This is the axis that
// catches *future* variants: rename the dropper all you like, an
// obfuscated blob wired into an ignition point still trips here.
func looksObfuscated(content []byte) []string {
	var markers []string
	lower := strings.ToLower(string(content))

	add := func(m string) { markers = append(markers, m) }

	// Dynamic-evaluation / decode primitives.
	if strings.Contains(lower, "eval(") {
		add("eval() call")
	}
	if strings.Contains(lower, "new function(") {
		add("dynamic Function() constructor")
	}
	if strings.Contains(lower, "atob(") || (strings.Contains(lower, "buffer.from(") && strings.Contains(lower, "base64")) {
		add("base64 decode at runtime")
	}
	if strings.Contains(lower, "fromcharcode") {
		add("String.fromCharCode packing")
	}
	if strings.Contains(lower, "child_process") || strings.Contains(lower, "execsync(") || strings.Contains(lower, "spawnsync(") {
		add("spawns a child process")
	}
	// Alternate-runtime loader to dodge Node-based monitoring (seen
	// in the reference payload). Specific tokens only — a bare "bun"
	// substring would false-positive on "bundle" etc.
	if strings.Contains(lower, "bunx") || strings.Contains(lower, "bun run") || strings.Contains(lower, "bun install") {
		add("invokes the Bun runtime")
	}

	// Dense hex / unicode escapes (\x41\x42… style packing).
	if escapeRuns := strings.Count(lower, `\x`) + strings.Count(lower, `\u`); escapeRuns >= 16 {
		add(fmt.Sprintf("dense \\x/\\u escapes (%d)", escapeRuns))
	}

	// A single very long line is the classic minified / one-blob tell.
	if longestLine(content) >= 2000 {
		add("very long single line (minified / one-blob payload)")
	}

	// High entropy over a non-trivial body — encrypted / packed.
	if len(content) >= 512 {
		if e := shannonEntropy(content); e >= 5.3 {
			add(fmt.Sprintf("high entropy (%.1f bits/byte)", e))
		}
	}

	return markers
}

// longestLine returns the length in bytes of the longest line in b.
func longestLine(b []byte) int {
	longest, cur := 0, 0
	for _, c := range b {
		if c == '\n' {
			if cur > longest {
				longest = cur
			}
			cur = 0
			continue
		}
		cur++
	}
	if cur > longest {
		longest = cur
	}
	return longest
}

// isTextContent reports whether b looks like text (valid UTF-8 with a
// low proportion of C0 control bytes). A config / hook file that
// decodes as binary is itself suspicious.
func isTextContent(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	ctrl := 0
	for _, c := range b {
		if c < 0x09 || (c > 0x0d && c < 0x20) {
			ctrl++
		}
	}
	return len(b) == 0 || float64(ctrl)/float64(len(b)) < 0.01
}

// ---------------------------------------------------------------------------
// Engine — pure scoring over gathered facts
// ---------------------------------------------------------------------------

// ignitionMatch records one catalog hit found while walking a branch's
// tree: the path, its blob size + SHA (from the tree entry), and the
// rule it matched.
type ignitionMatch struct {
	Path    string
	Size    int
	BlobSHA string
	Rule    ignitionRule
}

// scanBranch bundles a branch's tip provenance with the ignition
// matches found in its tree.
type scanBranch struct {
	Prov    BranchProvenance
	Matches []ignitionMatch
}

// scanInput is the gathered, network-sourced intermediate that
// evaluateScan reduces to a RepoScan. Splitting gather (FetchRepoScan)
// from score (evaluateScan) keeps the scoring engine a pure,
// table-testable function.
type scanInput struct {
	Owner, Name, URL string
	DefaultBranch    string
	BranchesTotal    int
	Truncated        bool
	Branches         []scanBranch
	Blobs            map[string]blobAnalysis // keyed by blob SHA
}

// evaluateScan is the pure heart of the detector: it turns gathered
// facts into weighted, explainable findings and a verdict. No I/O — so
// the whole scoring matrix is unit-tested without touching the
// network (see scan_test.go).
func evaluateScan(in scanInput) *RepoScan {
	s := &RepoScan{
		Owner:         in.Owner,
		Name:          in.Name,
		URL:           in.URL,
		DefaultBranch: in.DefaultBranch,
		BranchesTotal: in.BranchesTotal,
		Truncated:     in.Truncated,
	}

	// Signed-history baseline: only penalise an unsigned tip when the
	// repo otherwise signs (an all-unsigned repo just doesn't sign —
	// not a signal). Also note which branches carry any ignition file.
	// anySigned establishes the "this repo signs its commits" baseline
	// for the unsigned-delta signal — but ONLY genuine author
	// signatures count (Signed && !SignedByGitHub). A GitHub-signed tip
	// (a web-UI commit / squash-merge GitHub auto-signs) does NOT mean
	// the maintainer GPG-signs, so a gh-signed `main` next to an
	// ordinary unsigned feature branch must not read as a signing
	// delta — that was the false-positive the real (cleaned) victim
	// repo exposed.
	anySigned := false
	// branchHasWeighted tracks branches carrying a *meaningful* ignition
	// file (rule weight > 0 — a code-executing agent hook or a known
	// dropper), NOT a ubiquitous / prompt-only weight-0 surface.
	// Provenance anomalies (unsigned-delta) gate on this so an ordinary
	// unsigned feature branch doesn't trip the scan just because it
	// also carries the repo's CI workflows or a copilot-instructions
	// file.
	branchHasWeighted := map[string]bool{}
	for _, b := range in.Branches {
		s.Branches = append(s.Branches, b.Prov)
		if b.Prov.Signed && !b.Prov.SignedByGitHub {
			anySigned = true
		}
		for _, m := range b.Matches {
			if m.Rule.Weight > 0 {
				branchHasWeighted[b.Prov.Name] = true
			}
		}
	}
	s.BranchesScanned = len(in.Branches)

	add := func(f Finding) {
		f.Reason = Sanitize(f.Reason)
		f.Path = Sanitize(f.Path)
		f.Branch = Sanitize(f.Branch)
		s.Findings = append(s.Findings, f)
		s.Score += f.Weight
	}

	// Aggregate matches by path so a file present on many branches is
	// listed (and base-weighted) once, not once per branch. Without
	// this, a repo like bubbletea (8 workflows × 20 branches) emits 160
	// findings and a dropper on N branches would multiply its base
	// weight by N — both meaningless. Blob anomaly is then evaluated
	// per distinct content (blob SHA), provenance per branch.
	type matchAgg struct {
		rule      ignitionRule
		onDefault bool
		size      int
		bySHA     map[string][]string // blob SHA -> branches carrying that content
	}
	aggs := map[string]*matchAgg{}
	var pathOrder []string
	for _, b := range in.Branches {
		for _, m := range b.Matches {
			a := aggs[m.Path]
			if a == nil {
				a = &matchAgg{rule: m.Rule, bySHA: map[string][]string{}}
				aggs[m.Path] = a
				pathOrder = append(pathOrder, m.Path)
			}
			a.bySHA[m.BlobSHA] = append(a.bySHA[m.BlobSHA], b.Prov.Name)
			if m.Size > a.size {
				a.size = m.Size
			}
			if b.Prov.IsDefault {
				a.onDefault = true
			}
		}
	}

	branchHasBlobAnomaly := map[string]bool{}
	for _, p := range pathOrder {
		a := aggs[p]

		// Axis 1 — base ignition (one inventory entry per path).
		add(Finding{
			Axis:   AxisIgnition,
			Path:   p,
			Weight: a.rule.Weight,
			Reason: fmt.Sprintf("%s — %s", a.rule.Class, a.rule.Note),
		})

		// Axis 1 — side-branch divergence: a *meaningful* file (weight
		// > 0) that lives on side branches but never on the default
		// branch. A workflow added on a feature branch is normal and
		// must not score; a dropper that exists only on `next` is the
		// real signal.
		if !a.onDefault && a.rule.Weight > 0 {
			add(Finding{
				Axis:   AxisIgnition,
				Path:   p,
				Weight: wIgnitionNonDefaultOnly,
				Reason: fmt.Sprintf("present on %s but not on the default branch %q (divergence)", branchList(uniqueBranches(a.bySHA)), in.DefaultBranch),
			})
		}

		// Axis 2 — blob anomaly, once per distinct content.
		for sha, carriers := range a.bySHA {
			ba := in.Blobs[sha]
			anomalous := false
			if ba.Size > oversizeThreshold || a.size > oversizeThreshold {
				sz := ba.Size
				if sz == 0 {
					sz = a.size
				}
				add(Finding{Axis: AxisBlob, Path: p, Weight: wBlobOversized,
					Reason: fmt.Sprintf("oversized for its type: %s", humanBytes(sz))})
				anomalous = true
			}
			if len(ba.Markers) > 0 {
				add(Finding{Axis: AxisBlob, Path: p, Weight: wBlobObfuscated,
					Reason: "obfuscation markers: " + strings.Join(ba.Markers, ", ")})
				anomalous = true
			}
			if ba.Fetched && !ba.IsText {
				add(Finding{Axis: AxisBlob, Path: p, Weight: wBlobObfuscated,
					Reason: "binary content inside a text-config file"})
				anomalous = true
			}
			if anomalous {
				for _, br := range carriers {
					branchHasBlobAnomaly[br] = true
				}
			}
		}
	}

	// Axis 3 — provenance anomaly + the cross-axis smoking gun, per
	// branch (each forged / unsigned tip is its own evidence).
	for _, b := range in.Branches {
		provAnomaly := false
		switch {
		case b.Prov.Bot && !b.Prov.SignedByGitHub:
			// Forged: wears the GitHub-Actions identity but GitHub
			// didn't sign it. A real Actions commit is GitHub-signed.
			add(Finding{
				Axis:   AxisProvenance,
				Branch: b.Prov.Name,
				Weight: wProvSpoofIdentity,
				Reason: fmt.Sprintf("tip %s forged as %q but not signed by GitHub", shortOID(b.Prov.TipOID), identityOf(b.Prov)),
			})
			provAnomaly = true
		case anySigned && !b.Prov.Signed && (branchHasWeighted[b.Prov.Name] || branchHasBlobAnomaly[b.Prov.Name]):
			// Unsigned tip on a branch that carries a *meaningful*
			// ignition file or an anomalous blob, in a repo that
			// otherwise signs its commits. Gating on weight>0 / anomaly
			// keeps ordinary unsigned feature branches from scoring.
			add(Finding{
				Axis:   AxisProvenance,
				Branch: b.Prov.Name,
				Weight: wProvUnsignedDelta,
				Reason: fmt.Sprintf("tip %s is unsigned while the repo otherwise signs its commits", shortOID(b.Prov.TipOID)),
			})
			provAnomaly = true
		}

		if branchHasBlobAnomaly[b.Prov.Name] && provAnomaly {
			add(Finding{
				Axis:   AxisProvenance,
				Branch: b.Prov.Name,
				Weight: wCombinedSmokingGun,
				Reason: fmt.Sprintf("an anomalous payload and an anomalous commit tip coincide on branch %q", b.Prov.Name),
			})
		}
	}

	s.Verdict = verdictFor(s.Score)
	return s
}

// uniqueBranches returns the sorted, de-duplicated set of branch names
// across every content SHA of an aggregated path match.
func uniqueBranches(bySHA map[string][]string) []string {
	set := map[string]bool{}
	for _, branches := range bySHA {
		for _, b := range branches {
			set[b] = true
		}
	}
	out := make([]string, 0, len(set))
	for b := range set {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// branchList renders a branch-name set for a finding reason: a single
// name reads "branch \"x\"", several read "branches x, y, z".
func branchList(names []string) string {
	switch len(names) {
	case 0:
		return "a side branch"
	case 1:
		return fmt.Sprintf("branch %q", names[0])
	default:
		return "branches " + strings.Join(names, ", ")
	}
}

// identityOf returns the most specific identity string for a branch
// tip — login if GitHub linked one, else the raw committer/author
// name.
func identityOf(p BranchProvenance) string {
	switch {
	case p.AuthorLogin != "":
		return p.AuthorLogin
	case p.AuthorName != "":
		return p.AuthorName
	case p.CommitterName != "":
		return p.CommitterName
	default:
		return "unknown"
	}
}

// looksLikeBot reports whether any identity field on a tip looks like
// the GitHub-Actions bot — the identity the reference worm forged.
func looksLikeBot(authorName, committerName, authorLogin string) bool {
	for _, id := range []string{authorName, committerName, authorLogin} {
		l := strings.ToLower(strings.TrimSpace(id))
		if l == "github-actions" || l == "github-actions[bot]" || strings.HasPrefix(l, "github-actions") {
			return true
		}
	}
	return false
}

// shortOID truncates a git OID to the conventional 7-char short form.
func shortOID(oid string) string {
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}

// humanBytes renders a byte count compactly (KiB / MiB) for findings.
func humanBytes(n int) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.0f KiB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// ---------------------------------------------------------------------------
// Tier B — network gather (GraphQL refs + REST trees/blobs)
// ---------------------------------------------------------------------------

// scanBranchConcurrency bounds the per-branch REST tree fan-out, same
// discipline as watchedRepoConcurrency: enough to amortise serial
// latency, small enough that a many-branched repo can't burst-flood
// GitHub.
const scanBranchConcurrency = 8

// maxScanBranches caps how many branches we walk trees for. The
// default branch is always included; if a repo has more branches than
// this we scan the first maxScanBranches (default first) and flag the
// result Truncated. Most personal repos have a handful of branches;
// the cap is a safety net for fork-heavy or bot-managed repos.
const maxScanBranches = 20

// maxBlobFetches caps how many distinct matched-ignition blobs we pull
// for content analysis in a single scan.
const maxBlobFetches = 12

// scanRefsQuery enumerates a repository's branches with each tip's
// provenance (Axis 3) plus the tip's tree OID, which feeds the REST
// get-a-tree call (a real tree SHA, avoiding any ref-resolution
// ambiguity). One GraphQL round-trip regardless of branch count.
type scanRefsQuery struct {
	Repository struct {
		NameWithOwner    githubv4.String
		URL              githubv4.String `graphql:"url"`
		DefaultBranchRef *struct {
			Name githubv4.String
		}
		Refs struct {
			TotalCount githubv4.Int
			Nodes      []struct {
				Name   githubv4.String
				Target struct {
					Commit struct {
						OID             githubv4.GitObjectID `graphql:"oid"`
						MessageHeadline githubv4.String
						CommittedDate   githubv4.DateTime
						Tree            struct {
							OID githubv4.GitObjectID `graphql:"oid"`
						}
						Author struct {
							Name githubv4.String
							User *struct {
								Login githubv4.String
							}
						}
						Committer struct {
							Name githubv4.String
						}
						// Signature is null on unsigned commits. State is the
						// GitSignatureState enum (VALID / UNSIGNED / INVALID /
						// …) decoded as a string; WasSignedByGitHub separates a
						// genuine Actions commit from a forgery wearing its name.
						Signature *struct {
							IsValid           githubv4.Boolean
							State             githubv4.String
							WasSignedByGitHub githubv4.Boolean
						}
					} `graphql:"... on Commit"`
				}
			}
		} `graphql:"refs(refPrefix: \"refs/heads/\", first: 100, orderBy: {field: ALPHABETICAL, direction: ASC})"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// FetchRepoScan runs the on-demand supply-chain integrity scan for one
// repository and returns a scored, explainable RepoScan. Read-only:
// it inspects branches, file trees and matched-ignition blobs but
// never mutates the repo.
//
// Shape (one GraphQL branch + a bounded REST fan-out, the same idiom
// as FetchRepoDetail's GraphQL+REST split):
//
//  1. GraphQL scanRefsQuery — every branch tip's provenance + tree OID
//     in a single cheap round-trip (Axis 3).
//  2. REST get-a-tree per branch (recursive), bounded by
//     scanBranchConcurrency and maxScanBranches — match the ignition
//     catalog client-side, which is inherently generic (Axis 1) and
//     yields blob sizes for free (Axis 2 size).
//  3. REST get-a-blob for each distinct matched ignition file, bounded
//     by maxBlobFetches — entropy / obfuscation markers (Axis 2
//     content).
//
// Then the pure evaluateScan reduces the gathered facts to findings +
// verdict.
func (c *Client) FetchRepoScan(ctx context.Context, owner, name string) (*RepoScan, error) {
	var q scanRefsQuery
	vars := map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}
	if err := c.gql.Query(ctx, &q, vars); err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}

	repoURL := Sanitize(string(q.Repository.URL))
	defaultBranch := ""
	if q.Repository.DefaultBranchRef != nil {
		defaultBranch = Sanitize(string(q.Repository.DefaultBranchRef.Name))
	}

	// Build the provenance list and pick which branches to walk: the
	// default branch first, then the rest up to maxScanBranches.
	type branchPlan struct {
		prov    BranchProvenance
		treeOID string
	}
	var plans []branchPlan
	branchesTotal := int(q.Repository.Refs.TotalCount)
	for _, n := range q.Repository.Refs.Nodes {
		cm := n.Target.Commit
		bname := Sanitize(string(n.Name))
		prov := BranchProvenance{
			Name:          bname,
			IsDefault:     bname == defaultBranch,
			TipOID:        string(cm.OID),
			TipHeadline:   Sanitize(string(cm.MessageHeadline)),
			CommittedAt:   cm.CommittedDate.Time,
			AuthorName:    Sanitize(string(cm.Author.Name)),
			CommitterName: Sanitize(string(cm.Committer.Name)),
		}
		if cm.Author.User != nil {
			prov.AuthorLogin = Sanitize(string(cm.Author.User.Login))
		}
		if cm.Signature != nil {
			prov.SignatureState = Sanitize(string(cm.Signature.State))
			prov.Signed = bool(cm.Signature.IsValid)
			prov.SignedByGitHub = bool(cm.Signature.WasSignedByGitHub)
		}
		prov.Bot = looksLikeBot(prov.AuthorName, prov.CommitterName, prov.AuthorLogin)
		plans = append(plans, branchPlan{prov: prov, treeOID: string(cm.Tree.OID)})
	}

	// Default branch first so a truncated scan always covers it.
	sort.SliceStable(plans, func(i, j int) bool {
		if plans[i].prov.IsDefault != plans[j].prov.IsDefault {
			return plans[i].prov.IsDefault
		}
		return false
	})

	truncated := false
	if len(plans) > maxScanBranches {
		plans = plans[:maxScanBranches]
		truncated = true
	}

	// REST get-a-tree per branch, bounded fan-out. Dedupe by tree OID
	// so branches that share a tree (no divergence) are walked once.
	type treeResult struct {
		entries   []treeEntry
		truncated bool
	}
	treeCache := map[string]treeResult{}
	var treeMu sync.Mutex
	branches := make([]scanBranch, len(plans))

	sem := make(chan struct{}, scanBranchConcurrency)
	var wg sync.WaitGroup
	var fetchErr error
	var errMu sync.Mutex
	for i := range plans {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			p := plans[idx]
			treeMu.Lock()
			tr, cached := treeCache[p.treeOID]
			treeMu.Unlock()
			if !cached {
				entries, treeTrunc, err := c.fetchTree(ctx, owner, name, p.treeOID)
				if err != nil {
					errMu.Lock()
					if fetchErr == nil {
						fetchErr = err
					}
					errMu.Unlock()
					return
				}
				tr = treeResult{entries: entries, truncated: treeTrunc}
				treeMu.Lock()
				treeCache[p.treeOID] = tr
				treeMu.Unlock()
			}

			var matches []ignitionMatch
			for _, e := range tr.entries {
				if e.typ != "blob" {
					continue
				}
				if rule, ok := matchIgnition(e.path); ok {
					matches = append(matches, ignitionMatch{
						Path:    Sanitize(e.path),
						Size:    e.size,
						BlobSHA: e.sha,
						Rule:    rule,
					})
				}
			}
			branches[idx] = scanBranch{Prov: p.prov, Matches: matches}
		}(i)
	}
	wg.Wait()
	if fetchErr != nil {
		return nil, fetchErr
	}

	// A truncated tree means we may have missed a deeply-nested
	// ignition file; surface that honestly rather than implying full
	// coverage.
	for _, tr := range treeCache {
		if tr.truncated {
			truncated = true
		}
	}

	// REST get-a-blob for each distinct matched ignition file, bounded.
	blobs := map[string]blobAnalysis{}
	seen := map[string]bool{}
	fetched := 0
	for _, b := range branches {
		for _, m := range b.Matches {
			if seen[m.BlobSHA] {
				continue
			}
			seen[m.BlobSHA] = true
			ba := blobAnalysis{Size: m.Size}
			if m.Size <= maxBlobScanBytes && fetched < maxBlobFetches {
				content, err := c.fetchBlob(ctx, owner, name, m.BlobSHA)
				if err == nil {
					fetched++
					ba.Fetched = true
					ba.IsText = isTextContent(content)
					ba.Entropy = shannonEntropy(content)
					ba.Markers = looksObfuscated(content)
				}
				// A blob fetch failure is non-fatal: we still have the
				// size signal from the tree entry.
			}
			blobs[m.BlobSHA] = ba
		}
	}

	in := scanInput{
		Owner:         Sanitize(owner),
		Name:          Sanitize(name),
		URL:           repoURL,
		DefaultBranch: defaultBranch,
		BranchesTotal: branchesTotal,
		Truncated:     truncated,
		Branches:      branches,
		Blobs:         blobs,
	}
	return evaluateScan(in), nil
}

// treeEntry is one node from GitHub's get-a-tree response, reshaped.
type treeEntry struct {
	path string
	typ  string // "blob" | "tree"
	size int
	sha  string
}

// restTree mirrors the get-a-tree response shape.
type restTree struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int    `json:"size"`
		SHA  string `json:"sha"`
	} `json:"tree"`
	Truncated bool `json:"truncated"`
}

// fetchTree pulls a repository tree recursively by its tree SHA and
// returns the reshaped entries plus GitHub's own truncation flag
// (set when the tree exceeds the API's entry/size limits). Errors are
// wrapped in *FetchError with the shared classifyErr reasons so the
// scan surfaces the same actionable error screens as the rest of the
// app.
func (c *Client) fetchTree(ctx context.Context, owner, name, treeSHA string) ([]treeEntry, bool, error) {
	reqURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1",
		url.PathEscape(owner), url.PathEscape(name), url.PathEscape(treeSHA),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, false, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.rest.Do(req)
	if err != nil {
		return nil, false, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	defer resp.Body.Close()
	if err := restStatusError(resp); err != nil {
		return nil, false, err
	}

	var tree restTree
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, false, &FetchError{Reason: ReasonServer, Err: err}
	}
	entries := make([]treeEntry, 0, len(tree.Tree))
	for _, t := range tree.Tree {
		entries = append(entries, treeEntry{
			path: t.Path,
			typ:  t.Type,
			size: t.Size,
			sha:  t.SHA,
		})
	}
	return entries, tree.Truncated, nil
}

// restBlob mirrors the get-a-blob response (base64-encoded content).
type restBlob struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Size     int    `json:"size"`
}

// fetchBlob pulls one blob's content by SHA and returns the decoded
// bytes. Only called for matched ignition files within the size cap,
// so the payload stays bounded.
func (c *Client) fetchBlob(ctx context.Context, owner, name, sha string) ([]byte, error) {
	reqURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/git/blobs/%s",
		url.PathEscape(owner), url.PathEscape(name), url.PathEscape(sha),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.rest.Do(req)
	if err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	defer resp.Body.Close()
	if err := restStatusError(resp); err != nil {
		return nil, err
	}

	var blob restBlob
	if err := json.NewDecoder(resp.Body).Decode(&blob); err != nil {
		return nil, &FetchError{Reason: ReasonServer, Err: err}
	}
	if blob.Encoding != "base64" {
		// Unexpected encoding — treat as empty rather than guessing.
		return nil, nil
	}
	// GitHub wraps the base64 in newlines; strip them before decoding.
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if err != nil {
		return nil, &FetchError{Reason: ReasonServer, Err: err}
	}
	return decoded, nil
}

// restStatusError classifies a non-2xx REST response into the shared
// FetchError reasons, mirroring decodePRFilesPage so the scan's error
// screens stay consistent with the diff viewer's. Returns nil on 2xx.
func restStatusError(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	msg := Sanitize(strings.TrimSpace(string(body)))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &FetchError{Reason: ReasonRateLimitSecondary, Err: fmt.Errorf("github rest %s: %s", resp.Status, msg)}
	case resp.StatusCode == http.StatusForbidden:
		reason := ReasonRateLimitPrimary
		if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" && remaining != "0" {
			reason = ReasonRateLimitSecondary
		}
		return &FetchError{Reason: reason, Err: fmt.Errorf("github rest %s: %s", resp.Status, msg)}
	case resp.StatusCode == http.StatusUnauthorized:
		return &FetchError{Reason: ReasonAuth, Err: fmt.Errorf("github rest %s: %s", resp.Status, msg)}
	default:
		return &FetchError{Reason: ReasonServer, Err: fmt.Errorf("github rest %s: %s", resp.Status, msg)}
	}
}
