package github

import (
	"context"
	"strings"
	"testing"
)

// The body is the reason this fetch exists, and it is also the most
// attacker-controlled string the app renders: anyone who can share a gist
// link controls its content, and the content goes straight to a terminal.
//
// The escapes are JSON escapes because JSON forbids a raw control
// character inside a string — so this is the shape GitHub actually sends,
// and the raw byte only ever exists after the decoder expands it, which is
// exactly where Sanitize is waiting.
const gistDetailBody = `{"data":{"viewer":{"gist":{
	"name":"7ac536c358f737269b6b",
	"description":"settings",
	"url":"https://gist.github.com/gfazioli/7ac536c358f737269b6b",
	"isPublic":false,
	"files":[
		{"name":"main.go","size":42,"isTruncated":false,"isImage":false,
		 "language":{"name":"Go"},
		 "text":"package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"},
		{"name":"evil.txt","size":9,"isTruncated":true,"isImage":false,
		 "language":{"name":""},
		 "text":"before\u001b[31mafter\u009fend"},
		{"name":"logo.png","size":900,"isTruncated":false,"isImage":true,
		 "language":{"name":""},"text":""}
	]}}}}`

func TestFetchGistDetail(t *testing.T) {
	c := newTestGQLClient(t, 200, gistDetailBody)

	d, err := c.FetchGistDetail(context.Background(), "7ac536c358f737269b6b")
	if err != nil {
		t.Fatalf("FetchGistDetail: %v", err)
	}
	if d.Name != "7ac536c358f737269b6b" || d.IsPublic {
		t.Errorf("gist decoded wrong: %+v", d)
	}
	if len(d.Files) != 3 {
		t.Fatalf("files = %d, want 3", len(d.Files))
	}

	// Code has to survive readable. Sanitize keeps newlines and tabs, so
	// line structure and indentation are intact — stripping those would
	// turn every file into one unreadable line, which is the failure mode
	// that would make this whole view worthless.
	code := d.Files[0]
	if !strings.Contains(code.Text, "\n\tprintln") {
		t.Errorf("sanitising flattened the code — a newline or tab was dropped: %q", code.Text)
	}
	if code.Language != "Go" || code.Size != 42 {
		t.Errorf("file metadata decoded wrong: %+v", code)
	}

	// …while the sequences that would drive the terminal do not survive.
	evil := d.Files[1]
	if strings.ContainsRune(evil.Text, 0x1b) {
		t.Errorf("an ANSI escape reached the body: %q", evil.Text)
	}
	if strings.ContainsRune(evil.Text, 0x9f) {
		t.Errorf("a C1 introducer reached the body: %q", evil.Text)
	}
	if !strings.Contains(evil.Text, "before") || !strings.Contains(evil.Text, "end") {
		t.Errorf("sanitising ate the text around the escapes: %q", evil.Text)
	}

	// Truncation and binary arrive as facts, because the UI has to decline
	// rather than render nonsense — and neither can be inferred from size.
	if !evil.IsTruncated {
		t.Error("isTruncated did not decode; the UI would show a cut file as complete")
	}
	if !d.Files[2].IsImage {
		t.Error("isImage did not decode; the UI would print binary at the terminal")
	}
}

func TestFetchGistDetailSurfacesErrors(t *testing.T) {
	c := newTestGQLClient(t, 200, `{"errors":[{"message":"Could not resolve to a Gist"}]}`)
	if _, err := c.FetchGistDetail(context.Background(), "nope"); err == nil {
		t.Fatal("a GraphQL error decoded as success")
	}
}
