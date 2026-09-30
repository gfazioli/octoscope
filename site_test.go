package main

import (
	"encoding/xml"
	"html"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// What a crawler is served is only as right as the last hand that touched
// the site: there is no generator to keep a sitemap or a canonical in step
// with the pages. These tests hold the parts that went wrong unnoticed, both
// found by one audit on 2026-09-29: a sitemap that listed one page of twelve
// under a lastmod eleven weeks old, and eleven guide pages with no canonical.

const siteURL = "https://gfazioli.github.io/octoscope/"

// sitePages maps every HTML page under docs/ to the URL it is served at: a
// directory's index.html is served as the directory.
func sitePages(t *testing.T) map[string]string {
	t.Helper()
	pages := map[string]string{}
	err := filepath.WalkDir("docs", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		rel, err := filepath.Rel("docs", path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		url := siteURL + rel
		if rel == "index.html" || strings.HasSuffix(rel, "/index.html") {
			url = siteURL + strings.TrimSuffix(rel, "index.html")
		}
		pages[filepath.ToSlash(path)] = url
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("found no page under docs/ — the checks would measure nothing")
	}
	return pages
}

// TestSitemapListsEveryPage fails when a page under docs/ is missing from
// the sitemap, or the sitemap names a URL no page is served at. Adding a
// guide page means adding it here too, and nothing else would say so.
func TestSitemapListsEveryPage(t *testing.T) {
	var set struct {
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal([]byte(readDocsFile(t, "docs/sitemap.xml")), &set); err != nil {
		t.Fatalf("docs/sitemap.xml does not parse: %v", err)
	}
	listed := map[string]bool{}
	for _, u := range set.URLs {
		if listed[u.Loc] {
			t.Errorf("docs/sitemap.xml lists %s twice", u.Loc)
		}
		listed[u.Loc] = true
		// A date written by hand is a date that goes stale: this one
		// stayed at 2026-07-11 through eleven weeks of landing changes.
		if u.LastMod != "" {
			t.Errorf("docs/sitemap.xml gives %s a lastmod (%s); nothing here keeps one honest", u.Loc, u.LastMod)
		}
	}
	served := map[string]bool{}
	for path, url := range sitePages(t) {
		served[url] = true
		if !listed[url] {
			t.Errorf("%s is served at %s, which docs/sitemap.xml does not list", path, url)
		}
	}
	for url := range listed {
		if !served[url] {
			t.Errorf("docs/sitemap.xml lists %s, which no page under docs/ is served at", url)
		}
	}
}

// TestSitePagesAreIndexable holds every page to what a crawler reads first:
// one <main>, one <h1>, a description a search result can show whole, and
// a canonical that names the page's own URL — the one the sitemap lists.
func TestSitePagesAreIndexable(t *testing.T) {
	mainTag := regexp.MustCompile(`<main[\s>]`)
	h1Tag := regexp.MustCompile(`<h1[\s>]`)
	desc := regexp.MustCompile(`<meta name="description" content="([^"]*)"`)
	canonical := regexp.MustCompile(`<link rel="canonical" href="([^"]*)"`)
	for path, url := range sitePages(t) {
		page := readDocsFile(t, path)
		markup := markupOf(page)
		if n := len(mainTag.FindAllString(markup, -1)); n != 1 {
			t.Errorf("%s has %d <main> elements, want 1", path, n)
		}
		if n := len(h1Tag.FindAllString(markup, -1)); n != 1 {
			t.Errorf("%s has %d <h1> elements, want 1", path, n)
		}
		// A result's snippet is cut around 155–160 characters, wherever
		// the sentence happens to be; under 50 says too little to choose on.
		if m := desc.FindStringSubmatch(markup); m == nil {
			t.Errorf("%s has no meta description", path)
		} else if n := utf8.RuneCountInString(html.UnescapeString(m[1])); n < 50 || n > 160 {
			t.Errorf("%s has a %d-character description, want 50–160", path, n)
		}
		if got := canonical.FindAllStringSubmatch(markup, -1); len(got) != 1 || got[0][1] != url {
			t.Errorf("%s: want exactly one canonical, naming %s; got %v", path, url, got)
		}
	}
}
