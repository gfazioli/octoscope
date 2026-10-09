package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

// SweepSchemaVersion versions the `--scan --json` document on its own:
// it is a different contract from the dashboard report's, and the two
// are free to move independently.
const SweepSchemaVersion = 1

// sweepFindingsShown caps the scored findings listed per repository in
// --plain; --json carries them all.
const sweepFindingsShown = 3

// Sweep is the --scan report (#66): every repository the sweep was
// given, scanned or not, and a tally. It is the public wire contract of
// `octoscope --scan --json`. Every list is an array, never null.
type Sweep struct {
	SchemaVersion    int       `json:"schema_version"`
	OctoscopeVersion string    `json:"octoscope_version"`
	GeneratedAt      time.Time `json:"generated_at"`
	PublicOnly       bool      `json:"public_only"`
	// Scope says what each repository's scan covered: its default
	// branch only. The dashboard's scan walks every branch and compares
	// with the previous scan; the sweep does neither.
	Scope          string       `json:"scope"`
	ElapsedSeconds float64      `json:"elapsed_seconds"`
	Summary        SweepSummary `json:"summary"`
	Repositories   []SweepRepo  `json:"repositories"`
}

// SweepSummary counts the repositories by outcome. Every repository is
// in exactly one count, so the counts add up to Repositories.
type SweepSummary struct {
	Repositories      int `json:"repositories"`
	LikelyCompromised int `json:"likely_compromised"`
	Suspicious        int `json:"suspicious"`
	Watch             int `json:"watch"`
	Clean             int `json:"clean"`
	NotScanned        int `json:"not_scanned"`
}

// SweepRepo is one repository's outcome. When Scanned is false, Reason
// says why and the verdict fields are absent: a repository that was not
// read is never reported as clean.
type SweepRepo struct {
	Repository    string `json:"repository"`
	URL           string `json:"url"`
	Scanned       bool   `json:"scanned"`
	Reason        string `json:"reason,omitempty"`
	Verdict       string `json:"verdict,omitempty"`
	Score         *int   `json:"score,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	// Partial is set when the default branch's tree was too large for
	// GitHub to return whole, so a deeply nested file may be unseen.
	Partial  bool           `json:"partial"`
	Findings []SweepFinding `json:"findings"`
	// Unchecked names the capability probes that could not run, with
	// why: a clean verdict without them is a narrower claim.
	Unchecked []string `json:"unchecked"`
}

// SweepFinding is one piece of scored evidence behind a verdict.
type SweepFinding struct {
	Axis   string `json:"axis"`
	Path   string `json:"path,omitempty"`
	Weight int    `json:"weight"`
	Reason string `json:"reason"`
}

// FromSweep builds the report from the sweep's results, in the order
// they were given.
func FromSweep(results []github.SweepResult, octoscopeVersion string, generatedAt time.Time, elapsed time.Duration, publicOnly bool) Sweep {
	s := Sweep{
		SchemaVersion:    SweepSchemaVersion,
		OctoscopeVersion: octoscopeVersion,
		GeneratedAt:      generatedAt,
		PublicOnly:       publicOnly,
		Scope:            "default_branch",
		ElapsedSeconds:   float64(elapsed.Round(100*time.Millisecond)) / float64(time.Second),
		Repositories:     make([]SweepRepo, 0, len(results)),
	}
	s.Summary.Repositories = len(results)
	for _, r := range results {
		repo := SweepRepo{
			Repository: r.Target.Owner + "/" + r.Target.Name,
			URL:        r.Target.URL,
			Findings:   []SweepFinding{},
			Unchecked:  []string{},
		}
		if r.Scan == nil {
			repo.Reason = oneLine(r.NotScanned)
			s.Summary.NotScanned++
			s.Repositories = append(s.Repositories, repo)
			continue
		}
		scan := r.Scan
		score := scan.Score
		repo.Scanned = true
		repo.Verdict = verdictToken(scan.Verdict)
		repo.Score = &score
		repo.DefaultBranch = scan.DefaultBranch
		repo.Partial = scan.Truncated
		for _, f := range scan.ScoredFindings() {
			repo.Findings = append(repo.Findings, SweepFinding{Axis: string(f.Axis), Path: f.Path, Weight: f.Weight, Reason: f.Reason})
		}
		for _, u := range scan.Unchecked {
			repo.Unchecked = append(repo.Unchecked, u.Name+": "+u.Reason)
		}
		switch scan.Verdict {
		case github.VerdictCompromised:
			s.Summary.LikelyCompromised++
		case github.VerdictSuspicious:
			s.Summary.Suspicious++
		case github.VerdictWatch:
			s.Summary.Watch++
		default:
			s.Summary.Clean++
		}
		s.Repositories = append(s.Repositories, repo)
	}
	return s
}

// verdictToken is a verdict as the JSON carries it: the summary's key
// for it, so "likely compromised" reads likely_compromised in both
// places and a script can match it without a space.
func verdictToken(v github.ScanVerdict) string {
	return strings.ReplaceAll(v.String(), " ", "_")
}

// oneLine folds a reason onto one line and trims it, so an error that
// carried a response body does not flood the report.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:199]) + "…"
	}
	return s
}

// RenderSweepJSON writes the sweep as indented JSON.
func RenderSweepJSON(w io.Writer, s Sweep) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(s)
}

// RenderSweepPlain writes the sweep for a person: the tally, then every
// repository that is not clean — flagged ones with their strongest
// evidence, unscanned ones with why — then what the sweep did not look
// at. Clean repositories are counted, not listed; --json lists them.
// The layout is not a contract.
func RenderSweepPlain(w io.Writer, s Sweep) error {
	var b strings.Builder
	fmt.Fprintf(&b, "octoscope %s — supply-chain sweep\n", s.OctoscopeVersion)
	scope := "default branch only"
	if s.PublicOnly {
		scope += " · public-only"
	}
	fmt.Fprintf(&b, "%d %s · %s · %.1f s · generated %s\n\n",
		s.Summary.Repositories, plural(s.Summary.Repositories, "repository", "repositories"),
		scope, s.ElapsedSeconds, s.GeneratedAt.Format(time.RFC3339))

	for _, row := range []struct {
		label string
		n     int
	}{
		{"likely compromised", s.Summary.LikelyCompromised},
		{"suspicious", s.Summary.Suspicious},
		{"watch", s.Summary.Watch},
		{"clean", s.Summary.Clean},
		{"not scanned", s.Summary.NotScanned},
	} {
		fmt.Fprintf(&b, "  %-18s %4d\n", row.label, row.n)
	}

	for _, verdict := range []github.ScanVerdict{
		github.VerdictCompromised,
		github.VerdictSuspicious,
		github.VerdictWatch,
	} {
		var flagged []SweepRepo
		for _, r := range s.Repositories {
			if r.Scanned && r.Verdict == verdictToken(verdict) {
				flagged = append(flagged, r)
			}
		}
		if len(flagged) == 0 {
			continue
		}
		label := verdict.String()
		fmt.Fprintf(&b, "\n%s\n", strings.ToUpper(label[:1])+label[1:])
		for _, r := range flagged {
			fmt.Fprintf(&b, "  %s   score %d\n", r.Repository, *r.Score)
			for i, f := range r.Findings {
				if i == sweepFindingsShown {
					fmt.Fprintf(&b, "    … %d more in --json\n", len(r.Findings)-sweepFindingsShown)
					break
				}
				reason := oneLine(f.Reason)
				if f.Path != "" {
					// An ignition rule's reason names the class of file,
					// not the file: the path is what to go and look at.
					reason = f.Path + ": " + reason
				}
				fmt.Fprintf(&b, "    +%d %s  %s\n", f.Weight, f.Axis, reason)
			}
		}
	}

	var notScanned []SweepRepo
	for _, r := range s.Repositories {
		if !r.Scanned {
			notScanned = append(notScanned, r)
		}
	}
	if len(notScanned) > 0 {
		b.WriteString("\nNot scanned\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 3, ' ', 0)
		for _, r := range notScanned {
			fmt.Fprintf(tw, "  %s\t%s\n", r.Repository, r.Reason)
		}
		tw.Flush()
	}

	var partial []string
	unchecked := map[string]int{}
	for _, r := range s.Repositories {
		if r.Partial {
			partial = append(partial, r.Repository)
		}
		for _, u := range r.Unchecked {
			name, _, _ := strings.Cut(u, ":")
			unchecked[name]++
		}
	}
	if len(partial) > 0 {
		fmt.Fprintf(&b, "\nPartly read (tree too large for one answer): %s\n", strings.Join(partial, ", "))
	}
	if len(unchecked) > 0 {
		names := make([]string, 0, len(unchecked))
		for n := range unchecked {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, n := range names {
			parts = append(parts, fmt.Sprintf("%s on %d", n, unchecked[n]))
		}
		fmt.Fprintf(&b, "\nProbes that could not run (token scope): %s — reasons in --json\n", strings.Join(parts, ", "))
	}

	b.WriteString("\nDefault branch only, and no comparison with earlier scans. For every branch and what\n" +
		"changed since the last look, open a repository's scan in the dashboard (Repos tab, space, s).\n")
	_, err := io.WriteString(w, b.String())
	return err
}
