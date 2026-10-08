package main

import (
	"encoding/xml"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/net/html"
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
		XMLName xml.Name
		URLs    []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal([]byte(readDocsFile(t, "docs/sitemap.xml")), &set); err != nil {
		t.Fatalf("docs/sitemap.xml does not parse: %v", err)
	}
	// Any other root holds the same <url> entries and is not a sitemap.
	if set.XMLName.Local != "urlset" || set.XMLName.Space != "http://www.sitemaps.org/schemas/sitemap/0.9" {
		t.Errorf("docs/sitemap.xml's root is {%s}%s, want a sitemaps.org urlset", set.XMLName.Space, set.XMLName.Local)
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
	for path, url := range sitePages(t) {
		doc := pageOf(t, readDocsFile(t, path))
		if n := len(elements(doc, "main")); n != 1 {
			t.Errorf("%s has %d <main> elements, want 1", path, n)
		}
		if n := len(elements(doc, "h1")); n != 1 {
			t.Errorf("%s has %d <h1> elements, want 1", path, n)
		}
		var descs, canonicals []string
		for _, meta := range elements(doc, "meta") {
			if asciiLower(attr(meta, "name")) == "description" {
				descs = append(descs, attr(meta, "content"))
			}
		}
		for _, link := range elements(doc, "link") {
			for _, rel := range tokens(attr(link, "rel")) {
				if asciiLower(rel) == "canonical" {
					canonicals = append(canonicals, attr(link, "href"))
				}
			}
		}
		// A result's snippet is cut around 155–160 characters, wherever
		// the sentence happens to be; under 50 says too little to choose on.
		if len(descs) != 1 {
			t.Errorf("%s has %d meta descriptions, want 1", path, len(descs))
		} else if n := utf8.RuneCountInString(descs[0]); n < 50 || n > 160 {
			t.Errorf("%s has a %d-character description, want 50–160", path, n)
		}
		if len(canonicals) != 1 || canonicals[0] != url {
			t.Errorf("%s: want exactly one canonical, naming %s; got %q", path, url, canonicals)
		}
	}
}

// TestSitePagesServeTheirOwnFonts holds every page to the fonts in
// docs/fonts/. Each page has to link them itself: the stylesheets only name
// the families, so a page that forgets the link still renders, in the system
// font, and one survived a full review round that way. And no page may reach
// Google Fonts, which hands every visitor's address to Google: the fonts
// moved here on 2026-10-05 for exactly that.
func TestSitePagesServeTheirOwnFonts(t *testing.T) {
	const fontsCSS = "docs/fonts/fonts.css"
	for page := range sitePages(t) {
		src := readDocsFile(t, page)
		for _, host := range []string{"fonts.googleapis.com", "fonts.gstatic.com"} {
			if strings.Contains(src, host) {
				t.Errorf("%s still reaches %s", page, host)
			}
		}
		linked := 0
		for _, link := range elements(pageOf(t, src), "link") {
			for _, rel := range tokens(attr(link, "rel")) {
				if asciiLower(rel) == "stylesheet" && path.Join(path.Dir(page), attr(link, "href")) == fontsCSS {
					linked++
				}
			}
		}
		if linked != 1 {
			t.Errorf("%s links %s %d times, want once", page, fontsCSS, linked)
		}
	}
	// A face whose file is missing fails as quietly as a missing link. Every
	// quoting CSS allows is read: a face in double quotes, or in none, would
	// otherwise go unchecked.
	files := regexp.MustCompile(`url\(\s*['"]?([^'")\s]+)['"]?\s*\)`).FindAllStringSubmatch(readDocsFile(t, fontsCSS), -1)
	if len(files) == 0 {
		t.Fatalf("%s names no font file — the check would measure nothing", fontsCSS)
	}
	for _, f := range files {
		if _, err := os.Stat(path.Join(path.Dir(fontsCSS), f[1])); err != nil {
			t.Errorf("%s names %s, which is not there: %v", fontsCSS, f[1], err)
		}
	}
}

// TestSiteFitsAPhone guards the causes that made the landing and the guide
// scroll sideways at 320 and 360px (the landing measured 368px wide, the
// guide pages up to 394px). They are layout, so the real check is a browser
// at those widths; these are the shapes each fix took, so undoing one fails
// here instead of on a phone.
func TestSiteFitsAPhone(t *testing.T) {
	// The publisher line: the separators touch the words, so every one needs
	// a <wbr> after it, or "Fazioli·P.IVA …·Legal·Privacy" cannot wrap.
	doc := pageOf(t, readLanding(t))
	var legal *html.Node
	for _, d := range elements(doc, "div") {
		if slices.Contains(tokens(attr(d, "class")), "footer-legal") {
			legal = d
		}
	}
	if legal == nil {
		t.Fatal("no .footer-legal on the landing")
	}
	dots := 0
	for c := legal.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "span" || !slices.Contains(tokens(attr(c, "class")), "dot") {
			continue
		}
		dots++
		if n := c.NextSibling; n == nil || n.Type != html.ElementNode || n.Data != "wbr" {
			t.Errorf("a separator in the landing's publisher line has no <wbr> after it")
		}
	}
	if dots == 0 {
		t.Error("no separators found in the landing's publisher line")
	}
	if !strings.Contains(readDocsFile(t, "docs/guide/docs.js"), `·</span><wbr>'`) {
		t.Error("the guide's publisher line (docs.js) has no <wbr> after its separator")
	}

	// settings.html: option names wrap after an underscore, not mid-word, so
	// every underscore in the option column ends its text and a <wbr> follows.
	settings := pageOf(t, readDocsFile(t, "docs/guide/settings.html"))
	names := 0
	for _, td := range elements(settings, "td") {
		if !slices.Contains(tokens(attr(td, "class")), "k") {
			continue
		}
		for _, code := range elements(td, "code") {
			names++
			for c := code.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.TextNode {
					continue
				}
				i := strings.Index(c.Data, "_")
				if i < 0 {
					continue
				}
				n := c.NextSibling
				if i != len(c.Data)-1 || n == nil || n.Type != html.ElementNode || n.Data != "wbr" {
					t.Errorf("settings.html: an underscore in %q has no <wbr> after it", c.Data)
				}
			}
		}
	}
	if names == 0 {
		t.Fatal("no option names found in settings.html")
	}

	// The guide's stylesheet: what keeps the topbar, the pager, the option
	// table and inline code inside a 320px screen, checked by selector and
	// declaration so a reformat or an added property does not trip it.
	css := readDocsFile(t, "docs/guide/style.css")
	for _, want := range []struct{ media, selector, decl string }{
		{"", ".iconbtn", "flex: none"},
		{"", ".iconbtn", "white-space: nowrap"},
		{"", ".crumbs", "min-width: 0"},
		{"", ".crumbs", "text-overflow: ellipsis"},
		{"", "main.doc p > code, main.doc li > code", "overflow-wrap: anywhere"},
		{"max-width: 400px", ".topbar", "padding: 0 16px"},
		{"max-width: 400px", ".pager", "flex-direction: column"},
		{"max-width: 400px", ".keys td.k:has(wbr)", "white-space: normal"},
	} {
		if !slices.Contains(cssDecls(css, want.media, want.selector), want.decl) {
			t.Errorf("docs/guide/style.css: %s { %s } missing (media %q)", want.selector, want.decl, want.media)
		}
	}
}

// cssDecls returns the declarations of every rule for selector, top level
// when media is "" or inside each @media (<media>) block otherwise, each
// trimmed and with its whitespace collapsed. It reads this site's flat,
// hand-written stylesheets; it is not a CSS parser.
func cssDecls(css, media, selector string) []string {
	css = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	scopes := []string{css}
	if media != "" {
		scopes = nil
		open := "@media (" + media + ")"
		for rest := css; ; {
			i := strings.Index(rest, open)
			if i < 0 {
				break
			}
			body := rest[i+len(open):]
			body = body[strings.Index(body, "{")+1:]
			depth, j := 1, 0
			for ; j < len(body) && depth > 0; j++ {
				switch body[j] {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			scopes = append(scopes, body[:j])
			rest = body[j:]
		}
	} else {
		// Top level only: drop every @media block first.
		scopes = []string{regexp.MustCompile(`(?s)@media[^{]*\{(?:[^{}]*\{[^}]*\})*[^{}]*\}`).ReplaceAllString(css, "")}
	}
	rule := regexp.MustCompile(`(?:^|[}\s])` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`)
	space := regexp.MustCompile(`\s+`)
	var out []string
	for _, scope := range scopes {
		for _, m := range rule.FindAllStringSubmatch(scope, -1) {
			for _, d := range strings.Split(m[1], ";") {
				if d = strings.TrimSpace(space.ReplaceAllString(d, " ")); d != "" {
					out = append(out, d)
				}
			}
		}
	}
	return out
}
