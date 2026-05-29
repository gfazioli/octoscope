package github

import (
	"reflect"
	"testing"
)

// TestPublicStripsPrivateFromKnownLists pins the concrete behaviour of
// Stats.Public(): every list-bearing field drops its private entries
// while the public ones survive, and the per-repo aggregates are
// recomputed off the kept repos only.
func TestPublicStripsPrivateFromKnownLists(t *testing.T) {
	s := &Stats{
		Repositories: []Repo{
			{Name: "pub-a", Stars: 3, Forks: 1, OpenIssues: 2, OpenPRs: 1},
			{Name: "secret", Stars: 100, Forks: 50, OpenIssues: 9, OpenPRs: 9, IsPrivate: true},
			{Name: "pub-b", Stars: 4, Forks: 2, OpenIssues: 0, OpenPRs: 0},
		},
		OpenPullRequests: []PullRequest{
			{Number: 1, Title: "public pr", Repo: "owner/pub-a"},
			{Number: 2, Title: "private pr", Repo: "owner/secret", IsPrivate: true},
		},
		OpenIssuesList: []Issue{
			{Number: 10, Title: "public issue", Repo: "owner/pub-a"},
			{Number: 11, Title: "private issue", Repo: "owner/secret", IsPrivate: true},
		},
		WatchedRepos: []Repo{
			{Name: "watched-pub"},
			{Name: "watched-private", IsPrivate: true},
		},
		ReviewRequests: []PullRequest{
			{Number: 20, Title: "review me", Repo: "someone/pub", AuthorLogin: "alice"},
			{Number: 21, Title: "secret review", Repo: "someone/private", AuthorLogin: "bob", IsPrivate: true},
		},
	}

	got := s.Public()

	wantNames := func(field string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", field, got, want)
		}
	}

	repoNames := func(rs []Repo) []string {
		out := make([]string, len(rs))
		for i, r := range rs {
			out[i] = r.Name
		}
		return out
	}
	prTitles := func(ps []PullRequest) []string {
		out := make([]string, len(ps))
		for i, p := range ps {
			out[i] = p.Title
		}
		return out
	}

	wantNames("Repositories", repoNames(got.Repositories), []string{"pub-a", "pub-b"})
	wantNames("WatchedRepos", repoNames(got.WatchedRepos), []string{"watched-pub"})
	wantNames("OpenPullRequests", prTitles(got.OpenPullRequests), []string{"public pr"})
	wantNames("ReviewRequests", prTitles(got.ReviewRequests), []string{"review me"})

	if len(got.OpenIssuesList) != 1 || got.OpenIssuesList[0].Title != "public issue" {
		t.Errorf("OpenIssuesList = %+v, want only the public issue", got.OpenIssuesList)
	}

	// Aggregates recomputed off the kept (public) repos only.
	if got.TotalStars != 7 { // 3 + 4, not 107
		t.Errorf("TotalStars = %d, want 7 (private repo's 100 must not count)", got.TotalStars)
	}
	if got.ForksReceived != 3 { // 1 + 2
		t.Errorf("ForksReceived = %d, want 3", got.ForksReceived)
	}
	if got.PublicRepos != 2 {
		t.Errorf("PublicRepos = %d, want 2", got.PublicRepos)
	}
	if got.OpenPRsAuthored != 1 {
		t.Errorf("OpenPRsAuthored = %d, want 1", got.OpenPRsAuthored)
	}

	// The source must be untouched (Public returns a copy).
	if len(s.Repositories) != 3 || len(s.ReviewRequests) != 2 {
		t.Errorf("Public() mutated the source: repos=%d reviewReqs=%d", len(s.Repositories), len(s.ReviewRequests))
	}
}

// TestPublicStripsEveryPrivateList is a drift guard. It reflectively
// finds every slice field on Stats whose element type carries an
// IsPrivate bool, seeds each with one public + one private entry, and
// asserts Public() drops the private one from ALL of them. When a new
// private-aware list is added to Stats, this test seeds and checks it
// automatically — so forgetting the skip loop in Public() fails here
// instead of leaking in screenshot mode (the v0.x scar this guards).
func TestPublicStripsEveryPrivateList(t *testing.T) {
	fields := privateAwareSliceFields(reflect.TypeOf(Stats{}))
	if len(fields) < 5 {
		t.Fatalf("expected at least 5 private-aware list fields on Stats, found %d (%v) — "+
			"the reflection helper may be broken", len(fields), fieldNames(fields))
	}

	s := &Stats{}
	sv := reflect.ValueOf(s).Elem()
	for _, f := range fields {
		fv := sv.Field(f.idx)
		sl := reflect.MakeSlice(fv.Type(), 2, 2)
		sl.Index(0).FieldByName("IsPrivate").SetBool(false) // public, must survive
		sl.Index(1).FieldByName("IsPrivate").SetBool(true)  // private, must be dropped
		fv.Set(sl)
	}

	got := s.Public()
	gv := reflect.ValueOf(got).Elem()
	for _, f := range fields {
		fv := gv.Field(f.idx)
		if fv.Len() == 0 {
			t.Errorf("Public() dropped every entry from %s; the public one should survive", f.name)
			continue
		}
		for j := 0; j < fv.Len(); j++ {
			if fv.Index(j).FieldByName("IsPrivate").Bool() {
				t.Errorf("Public() left a private entry in %s — add a skip loop for it in Public()", f.name)
				break
			}
		}
	}
}

type sliceField struct {
	idx  int
	name string
}

// privateAwareSliceFields returns the index+name of every exported
// slice field on t whose element is a struct with a bool IsPrivate
// field — i.e. every list that can carry a private item.
func privateAwareSliceFields(t reflect.Type) []sliceField {
	var out []sliceField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type.Kind() != reflect.Slice {
			continue
		}
		elem := f.Type.Elem()
		if elem.Kind() != reflect.Struct {
			continue
		}
		if pf, ok := elem.FieldByName("IsPrivate"); ok && pf.Type.Kind() == reflect.Bool {
			out = append(out, sliceField{idx: i, name: f.Name})
		}
	}
	return out
}

func fieldNames(fs []sliceField) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.name
	}
	return out
}
