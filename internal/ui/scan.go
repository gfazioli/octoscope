package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// ScanModel is the supply-chain integrity-scan drill-in for a single
// repository. It follows the canonical drill-in shape (see
// repo_detail.go): a sticky title row + a viewport-wrapped body, with
// loading / error / loaded states, reached via the Repos action menu
// ("Security scan", shortcut `s`).
//
// Read-only by construction: the loaded report explains findings and
// hands the user a copy-paste remediation script + the right revoke
// links, but octoscope never mutates the repository (design principle
// 3 / CLAUDE.md "Out of scope").
type ScanModel struct {
	open    bool
	repo    github.Repo
	scan    *github.RepoScan
	err     error
	loading bool

	viewport viewport.Model
}

// IsOpen reports whether the scan view is active — used by the root
// model to route keystrokes here and to replace the Repos tab body.
func (sm ScanModel) IsOpen() bool { return sm.open }

// Open returns a fresh scan model in the loading state, seeded with
// the row the user picked. The caller dispatches fetchRepoScanCmd
// alongside this so the loading state transitions.
func (sm ScanModel) Open(repo github.Repo) ScanModel {
	return ScanModel{
		open:     true,
		repo:     repo,
		loading:  true,
		viewport: viewport.New(0, 0),
	}
}

// Close returns a closed scan model (zero value).
func (sm ScanModel) Close() ScanModel { return ScanModel{} }

// Update handles one key event while the scan view is open. Action
// keys (q / esc / r / o / y) take precedence; everything else scrolls
// the viewport. `y` copies the remediation script — offered only once
// a loaded scan has actually flagged something, so the key is never
// advertised when there's nothing to remediate.
func (sm ScanModel) Update(msg tea.KeyMsg, client *github.Client, width, height int) (ScanModel, tea.Cmd) {
	if !sm.open {
		return sm, nil
	}
	switch msg.String() {
	case "q":
		return sm.Close(), tea.Quit
	case "esc":
		return sm.Close(), nil
	case "o":
		return sm, openURLCmd(sm.repo.URL)
	case "r":
		owner, name := github.SplitOwnerName(sm.repo.URL)
		if owner == "" {
			return sm, nil
		}
		sm.loading = true
		sm.err = nil
		sm.scan = nil
		return sm, fetchRepoScanCmd(client, owner, name, sm.repo.URL)
	case "y":
		if sm.scan != nil && sm.scan.Verdict >= github.VerdictSuspicious {
			return sm, copyTextCmd(remediationScript(sm.scan), "Script")
		}
	}

	sm = sm.syncViewport(width, height)
	var cmd tea.Cmd
	sm.viewport, cmd = sm.viewport.Update(msg)
	return sm, cmd
}

// syncViewport refreshes the viewport content + dimensions. No-op in
// the transient states (their bodies are short and always fit).
func (sm ScanModel) syncViewport(width, height int) ScanModel {
	if sm.loading || sm.err != nil || sm.scan == nil {
		return sm
	}
	sm.viewport.Width = width
	sm.viewport.Height = bodyViewportHeight(height)
	sm.viewport.SetContent(sm.computeBody(width))
	return sm
}

// applyFetched commits a fetched scan (or error). The root checks the
// URL correlation before calling this so a stale response can't
// clobber a re-opened scan.
func (sm ScanModel) applyFetched(scan *github.RepoScan, err error) ScanModel {
	sm.loading = false
	sm.scan = scan
	sm.err = err
	return sm
}

// View renders the scan inside the tab content area: sticky title +
// viewport-wrapped body. Mirrors RepoDetailModel.View.
func (sm ScanModel) View(width, height int) string {
	if !sm.open {
		return ""
	}
	title := sm.renderTitle()

	if sm.loading {
		_, name := github.SplitOwnerName(sm.repo.URL)
		return title + "\n\n" +
			mutedStyle.Render("Scanning "+name+" for supply-chain integrity issues…")
	}
	if sm.err != nil {
		return title + "\n\n" +
			errorStyle.Render("Could not complete the scan") + "\n" +
			mutedStyle.Render(sm.err.Error()) + "\n\n" +
			keyHints("r", "retry", "esc", "back")
	}
	if sm.scan == nil {
		return title + "\n\n" + mutedStyle.Render("(no data)")
	}

	body := sm.computeBody(width)
	if height <= 0 {
		return title + "\n\n" + body
	}
	vp := sm.viewport
	vp.Width = width
	vp.Height = bodyViewportHeight(height)
	vp.SetContent(body)
	return title + "\n\n" + vp.View()
}

// renderTitle is the sticky breadcrumb + key hints. The `y` hint
// appears only when there's something to remediate.
func (sm ScanModel) renderTitle() string {
	owner, name := github.SplitOwnerName(sm.repo.URL)
	titleText := fmt.Sprintf("▸ Repos / %s / %s / security scan", owner, name)
	hints := []string{
		"esc", "back",
		"o", "open in github",
		"r", "rescan",
	}
	if sm.scan != nil && sm.scan.Verdict >= github.VerdictSuspicious {
		hints = append(hints, "y", "copy fix script")
	}
	return activeTabStyle.Render(titleText) + "  " + keyHints(hints...)
}

// computeBody renders the loaded report: verdict headline, scan
// summary, scored findings, ignition-surface inventory, per-branch
// provenance, and (when warranted) the remediation panel.
func (sm ScanModel) computeBody(width int) string {
	s := sm.scan
	var b strings.Builder

	// ---- Verdict headline
	glyph, style := scanVerdictStyle(s.Verdict)
	headline := style.Render(fmt.Sprintf("%s  %s", glyph, strings.ToUpper(s.Verdict.String())))
	b.WriteString(headline)
	if s.Score > 0 {
		b.WriteString("   " + mutedStyle.Render(fmt.Sprintf("risk score %d", s.Score)))
	}
	b.WriteString("\n")

	// ---- Scan summary
	summary := fmt.Sprintf("Scanned %d of %d branches", s.BranchesScanned, s.BranchesTotal)
	if s.Truncated {
		summary += " (bounded — some branches / deep trees not fully walked)"
	}
	b.WriteString(mutedStyle.Render(summary))
	b.WriteString("\n\n")

	// ---- Scored findings (the evidence behind the verdict)
	if scored := s.ScoredFindings(); len(scored) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Findings"))
		b.WriteString("\n")
		b.WriteString(renderFindings(scored, width))
		b.WriteString("\n\n")
	} else {
		b.WriteString(okStyle.Render("No anomalies scored.") +
			mutedStyle.Render(" The auto-execution surface below is informational."))
		b.WriteString("\n\n")
	}

	// ---- Ignition-surface inventory (always shown — know your surface)
	if inv := s.IgnitionInventory(); len(inv) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Auto-execution surface present"))
		b.WriteString("\n")
		b.WriteString(renderIgnitionInventory(inv, width))
		b.WriteString("\n\n")
	}

	// ---- Per-branch provenance
	if len(s.Branches) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Branch tips"))
		b.WriteString("\n")
		b.WriteString(renderBranchProvenance(s.Branches, width))
		b.WriteString("\n\n")
	}

	// ---- Remediation (only when there's something to act on)
	if s.Verdict >= github.VerdictSuspicious {
		b.WriteString(renderRemediation(s, width))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// scanVerdictStyle pairs each verdict with a distinct glyph and a
// theme style. The glyph carries the meaning even in monochromatic
// themes where the colours sit close together — same discipline as
// the CI rollup dot (CLAUDE.md monochrome contract).
func scanVerdictStyle(v github.ScanVerdict) (string, lipgloss.Style) {
	switch v {
	case github.VerdictClean:
		return "✓", okStyle.Bold(true)
	case github.VerdictWatch:
		return "•", watchStyle
	case github.VerdictSuspicious:
		return "⚠", warnStyle.Bold(true)
	case github.VerdictCompromised:
		return "✕", errorStyle
	default:
		return "?", mutedStyle
	}
}

// renderFindings lays out the scored evidence: an axis tag, the
// reason, the branch (when branch-specific) and the weight it added.
func renderFindings(findings []github.Finding, width int) string {
	var lines []string
	for _, f := range findings {
		tag := mutedStyle.Render("[" + string(f.Axis) + "]")
		weight := warnStyle.Render(fmt.Sprintf("+%d", f.Weight))
		reason := f.Reason
		if f.Branch != "" {
			reason = fmt.Sprintf("%s  %s", reason, mutedStyle.Render("· "+f.Branch))
		}
		line := fmt.Sprintf("  %s %s  %s", weight, tag, reason)
		lines = append(lines, lipgloss.NewStyle().Width(max(width-2, 20)).Render(line))
	}
	return strings.Join(lines, "\n")
}

// renderIgnitionInventory lists every auto-execution surface found,
// regardless of score, so the user can audit their attack surface.
func renderIgnitionInventory(findings []github.Finding, width int) string {
	// Dedupe by path so the same file on multiple branches lists once.
	seen := map[string]bool{}
	var lines []string
	for _, f := range findings {
		if f.Path == "" || seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		path := valueStyle.Render(f.Path)
		line := fmt.Sprintf("  %s  %s", path, mutedStyle.Render(f.Reason))
		lines = append(lines, lipgloss.NewStyle().Width(max(width-2, 20)).Render(line))
	}
	if len(lines) == 0 {
		return mutedStyle.Render("  (none)")
	}
	return strings.Join(lines, "\n")
}

// renderBranchProvenance renders the per-branch tip table: name (with
// a default-branch marker), short OID, signature state, and identity.
func renderBranchProvenance(branches []github.BranchProvenance, width int) string {
	// Stable order: default branch first, then alphabetical.
	rows := make([]github.BranchProvenance, len(branches))
	copy(rows, branches)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].IsDefault != rows[j].IsDefault {
			return rows[i].IsDefault
		}
		return rows[i].Name < rows[j].Name
	})

	const (
		nameW = 22
		oidW  = 9
		sigW  = 12
	)
	var lines []string
	for _, p := range rows {
		name := p.Name
		if p.IsDefault {
			name += " *"
		}
		nameCol := padRight(truncate(name, nameW), nameW)
		oidCol := mutedStyle.Render(padRight(shortBranchOID(p.TipOID), oidW))

		sig := signatureLabel(p)
		sigStyle := mutedStyle
		switch {
		case p.Bot && !p.SignedByGitHub:
			sigStyle = errorStyle
		case p.Signed:
			sigStyle = okStyle
		case !p.Signed:
			sigStyle = warnStyle
		}
		sigCol := sigStyle.Render(padRight(sig, sigW))

		identity := p.AuthorLogin
		if identity == "" {
			identity = p.AuthorName
		}
		idCol := mutedStyle.Render(truncate(identity, max(width-nameW-oidW-sigW-8, 8)))

		lines = append(lines, "  "+nameCol+"  "+oidCol+"  "+sigCol+"  "+idCol)
	}
	return strings.Join(lines, "\n")
}

// signatureLabel summarises a tip's signature state for the table.
func signatureLabel(p github.BranchProvenance) string {
	switch {
	case p.Bot && !p.SignedByGitHub:
		return "forged"
	case p.SignedByGitHub:
		return "gh-signed"
	case p.Signed:
		return "signed"
	default:
		return "unsigned"
	}
}

// shortBranchOID renders a 7-char short OID, or a placeholder when the
// tip OID is missing.
func shortBranchOID(oid string) string {
	if len(oid) >= 7 {
		return oid[:7]
	}
	if oid == "" {
		return "—"
	}
	return oid
}

// renderRemediation renders the read-only remediation panel: the safe
// next steps, the revoke links, and a pointer to the `y` copy-script
// key. Bordered in the verdict colour so it reads as the call to
// action.
func renderRemediation(s *github.RepoScan, width int) string {
	_, style := scanVerdictStyle(s.Verdict)
	border := colWarn
	if s.Verdict >= github.VerdictCompromised {
		border = colError
	}

	var lines []string
	lines = append(lines, style.Render("Remediation — octoscope is read-only, these are steps for you to run"))
	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("1. Inspect without executing:  git clone --no-checkout "+s.URL))
	lines = append(lines, mutedStyle.Render("2. Reset (not revert) affected branches to a clean parent, then force-push"))
	lines = append(lines, mutedStyle.Render("3. Ask GitHub Support to GC the malicious commit SHAs from the fork network"))
	lines = append(lines, mutedStyle.Render("4. Revoke the OAuth authorization grant, not just the token:"))
	lines = append(lines, "     "+valueStyle.Render("https://github.com/settings/applications"))
	lines = append(lines, "     "+valueStyle.Render("https://github.com/settings/tokens"))
	lines = append(lines, "")
	lines = append(lines, boldStyle.Foreground(colAccent).Render("Press y")+mutedStyle.Render(" to copy a ready-to-run remediation script to your clipboard."))

	boxW := max(width-4, 30)
	return remediationBoxStyle(border, boxW).Render(strings.Join(lines, "\n"))
}

// remediationScript builds the plain-text shell script copied to the
// clipboard by `y`. It is filled with the repo's URL and the actual
// affected branches / ignition paths the scan found, but is otherwise
// inert text — octoscope hands it over, it never runs it.
func remediationScript(s *github.RepoScan) string {
	owner, name := s.Owner, s.Name

	// Collect affected branches + the ignition paths flagged on them.
	branchSet := map[string]bool{}
	pathSet := map[string]bool{}
	for _, f := range s.ScoredFindings() {
		if f.Branch != "" {
			branchSet[f.Branch] = true
		}
		if f.Path != "" {
			pathSet[f.Path] = true
		}
	}
	branches := sortedKeys(branchSet)
	paths := sortedKeys(pathSet)

	var b strings.Builder
	fmt.Fprintf(&b, "#!/usr/bin/env bash\n")
	fmt.Fprintf(&b, "# octoscope supply-chain remediation for %s/%s\n", owner, name)
	fmt.Fprintf(&b, "# Verdict: %s (risk score %d). Review every step before running.\n", s.Verdict, s.Score)
	fmt.Fprintf(&b, "set -euo pipefail\n\n")

	fmt.Fprintf(&b, "# 1. Mirror the repo WITHOUT checking it out (never execute the payload).\n")
	fmt.Fprintf(&b, "git clone --no-checkout %s inspect-%s\n", s.URL, name)
	fmt.Fprintf(&b, "cd inspect-%s\n\n", name)

	fmt.Fprintf(&b, "# 2. Confirm the implant across every branch (by content, not by author).\n")
	if len(paths) > 0 {
		// Paths go into a single-quote-escaped bash array so a hostile
		// path can't break out of the generated script; branches are
		// iterated via for-each-ref (no word-splitting, and the
		// symbolic origin/HEAD is filtered out).
		fmt.Fprintf(&b, "paths=(")
		for i, p := range paths {
			if i > 0 {
				fmt.Fprint(&b, " ")
			}
			fmt.Fprint(&b, bashSingleQuote(p))
		}
		fmt.Fprintf(&b, ")\n")
		fmt.Fprintf(&b, "for b in $(git for-each-ref --format='%%(refname:short)' refs/remotes/origin | grep -v '^origin/HEAD$'); do\n")
		fmt.Fprintf(&b, "  for p in \"${paths[@]}\"; do\n")
		fmt.Fprintf(&b, "    git cat-file -e \"$b:$p\" 2>/dev/null && echo \"  ^ $p on $b\"\n")
		fmt.Fprintf(&b, "  done\n")
		fmt.Fprintf(&b, "done\n\n")
	} else {
		fmt.Fprintf(&b, "# (no specific payload path flagged — inspect the findings in octoscope)\n\n")
	}

	fmt.Fprintf(&b, "# 3. RESET (not revert) the affected branches to a clean parent, then force-push.\n")
	fmt.Fprintf(&b, "#    A revert leaves the payload retrievable at the old commit.\n")
	if len(branches) > 0 {
		for _, br := range branches {
			fmt.Fprintf(&b, "#    git push --force origin <clean-sha>:%s\n", br)
		}
	} else {
		fmt.Fprintf(&b, "#    git push --force origin <clean-sha>:<branch>\n")
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "# 4. Ask GitHub Support to garbage-collect the malicious commit SHAs.\n")
	fmt.Fprintf(&b, "# 5. REVOKE THE OAUTH GRANT (not just the token):\n")
	fmt.Fprintf(&b, "#    https://github.com/settings/applications\n")
	fmt.Fprintf(&b, "#    https://github.com/settings/tokens\n")

	return b.String()
}

// bashSingleQuote wraps s in single quotes for safe interpolation into
// the generated remediation script, escaping any embedded single quote
// via the standard '\” trick. The script is copy-paste text the user
// runs, so a hostile path must not be able to break out of it.
func bashSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sortedKeys returns the keys of a map in stable, sorted order.
// Generic over the value type so it serves both the scan's string
// sets and viewprefs' enum maps (WorkFilterKeys / StarHistoryKeys).
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// max is the int sibling of the min helper in repo_detail.go — Go's
// builtin max exists since 1.21 but lipgloss width math reads clearer
// with a guarded local here.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- root-model plumbing -------------------------------------------------

// viewRepoScanMsg is fired by the "Security scan" menu entry on a
// Repos row. The root model intercepts it to open the scan drill-in
// and fire the targeted fetch (mirror of viewRepoDetailMsg).
type viewRepoScanMsg struct {
	repo github.Repo
}

// repoScanFetchedMsg carries the FetchRepoScan result back to Update.
// The URL field is the stale-fetch correlation key.
type repoScanFetchedMsg struct {
	url  string
	scan *github.RepoScan
	err  error
}

// viewRepoScanCmd captures a Repos row for the action menu.
func viewRepoScanCmd(r github.Repo) tea.Cmd {
	return func() tea.Msg {
		return viewRepoScanMsg{repo: r}
	}
}

// scanFetchTimeout is generous: a scan is a bounded REST fan-out (one
// tree per branch + a few blobs) on top of one GraphQL round-trip, so
// it can run longer than a single detail query on a many-branched
// repo. Matches the dashboard fetch ceiling.
const scanFetchTimeout = 30 * time.Second

// fetchRepoScanCmd builds the BubbleTea command that runs the scan off
// the network and returns a repoScanFetchedMsg.
func fetchRepoScanCmd(client *github.Client, owner, name, url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), scanFetchTimeout)
		defer cancel()
		scan, err := client.FetchRepoScan(ctx, owner, name)
		return repoScanFetchedMsg{url: url, scan: scan, err: err}
	}
}
