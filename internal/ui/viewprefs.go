package ui

import "sort"

// View-preference config keys (#35). default_sort / default_work_filter
// / default_star_history travel as raw strings through config → main.go
// → Options (the config package can't import ui without a cycle), so
// the canonical key names and their enum mappings live here, next to
// the enums they seed. main.go validates with the IsValid* helpers and
// rejects unknown non-empty values at startup — the same layering as
// the theme name.

// reposSortKeys maps config-facing sort keys to the Repos tab cycle.
// Plain-ASCII names, no glyphs. A missing key yields the zero value
// (ReposSortPushed) — the built-in default.
var reposSortKeys = map[string]ReposSort{
	"pushed":  ReposSortPushed,
	"stars":   ReposSortStars,
	"forks":   ReposSortForks,
	"name":    ReposSortName,
	"ci":      ReposSortCI,
	"release": ReposSortRelease,
}

// listSortKeys maps the sort keys shared by the PRs and Issues tabs
// (the two cycles are parallel by design). A missing key yields the
// zero values (updated / updated) — the built-in defaults.
var listSortKeys = map[string]struct {
	prs    PRsSort
	issues IssuesSort
}{
	"updated": {PRsSortUpdated, IssuesSortUpdated},
	"repo":    {PRsSortRepo, IssuesSortRepo},
	"number":  {PRsSortNumber, IssuesSortNumber},
}

// workFilterKeys maps config-facing keys to the Repos work-filter
// presets; the names mirror the header chips.
var workFilterKeys = map[string]WorkFilter{
	"prs-open":  WorkFilterPRsOpen,
	"ci-broken": WorkFilterCIBroken,
	"stale":     WorkFilterStale,
}

// starHistoryKeys maps config-facing keys to the star-history
// sparkline reducer modes.
var starHistoryKeys = map[string]StarHistoryMode{
	"density":    StarModeDensity,
	"cumulative": StarModeCumulative,
}

// IsValidSortKey reports whether s names a sort column on at least
// one list tab. One key seeds every tab whose cycle has that column
// ("stars" → Repos; "updated" → PRs and Issues); tabs without it
// keep their built-in default.
func IsValidSortKey(s string) bool {
	if _, ok := reposSortKeys[s]; ok {
		return true
	}
	_, ok := listSortKeys[s]
	return ok
}

// IsValidWorkFilterKey reports whether s names a Repos work-filter
// preset.
func IsValidWorkFilterKey(s string) bool {
	_, ok := workFilterKeys[s]
	return ok
}

// IsValidStarHistoryKey reports whether s names a star-history
// sparkline mode.
func IsValidStarHistoryKey(s string) bool {
	_, ok := starHistoryKeys[s]
	return ok
}

// SortKeys returns every accepted default_sort value, sorted, for
// startup error messages. It unions the two sort maps (a key is valid
// on whichever tab(s) own that column — see IsValidSortKey), which
// hold different enum value types, so the merge stays explicit rather
// than going through the single-map sortedKeys helper.
func SortKeys() []string {
	keys := make([]string, 0, len(reposSortKeys)+len(listSortKeys))
	for k := range reposSortKeys {
		keys = append(keys, k)
	}
	for k := range listSortKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// WorkFilterKeys returns every accepted default_work_filter value,
// sorted, for startup error messages.
func WorkFilterKeys() []string { return sortedKeys(workFilterKeys) }

// StarHistoryKeys returns every accepted default_star_history value,
// sorted, for startup error messages.
func StarHistoryKeys() []string { return sortedKeys(starHistoryKeys) }
