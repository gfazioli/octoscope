package ui

import (
	"net/url"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// hyperlink wraps label in an OSC 8 terminal hyperlink pointing at url.
// Terminals that support OSC 8 render label as a clickable link;
// terminals that don't ignore the escapes and print label verbatim, so
// the visible text — and the ansi.Strip output — is always just label, a
// safe fallback. An empty url returns label unwrapped. Width math is
// unaffected: lipgloss.Width / ansi.Strip both ignore OSC 8.
//
// NOTE: only use this on TRUSTED urls (hardcoded consts). For a
// GitHub-sourced URL use githubHyperlink, which validates the URI before
// it reaches the escape sequence — an unvalidated URI inside an OSC 8
// escape is a terminal-injection vector, and a non-GitHub target on a
// GitHub-looking row is a phishing vector.
func hyperlink(rawURL, label string) string {
	if rawURL == "" {
		return label
	}
	return ansi.SetHyperlink(rawURL) + label + ansi.ResetHyperlink()
}

// githubHyperlink is hyperlink for URLs that came back from the GitHub
// API. It links label only when the URL passes isGitHubURL; otherwise it
// returns label as plain text, so a URL we don't vouch for degrades to
// something inert rather than becoming a clickable escape sequence.
//
// Callers still pass strings that went through github.Sanitize at the
// extractor boundary — this is the second gate, not the first.
func githubHyperlink(rawURL, label string) string {
	if !isGitHubURL(rawURL) {
		return label
	}
	return hyperlink(rawURL, label)
}

// isGitHubURL reports whether rawURL is safe to embed in an OSC 8
// escape: an absolute https URL on a github.com host, with nothing in
// it that could terminate the escape early or smuggle a second one.
//
// Three separate concerns, all of which have to hold:
//
//   - **Escape integrity.** OSC 8 is `ESC ] 8 ; params ; URI ST`. A
//     control byte, a `;`, or an ESC inside the URI can close the
//     sequence early and let the rest of the string be interpreted as
//     terminal commands. Sanitize already strips C0/C1 and ANSI, but a
//     `;` is an ordinary character it has no reason to touch — so it
//     gets rejected here. GitHub's own run / check URLs never contain
//     one.
//   - **Scheme.** Only https. `javascript:`, `file:`, `data:` and
//     friends have no business in a terminal hyperlink, and some
//     terminals hand the URI straight to the OS opener.
//   - **Host.** github.com or a subdomain of it. A check's detailsUrl
//     can legitimately point off-platform (a third-party CI provider
//     reporting through the Checks API), and those are exactly the
//     targets we don't want to make one-click from a row that looks
//     like it belongs to GitHub. They render as plain text instead.
func isGitHubURL(rawURL string) bool {
	if rawURL == "" || len(rawURL) > 2048 {
		return false
	}
	// Reject anything that could break out of the escape sequence
	// before handing the string to url.Parse — cheaper and stricter
	// than reasoning about what the parser normalises.
	if strings.ContainsAny(rawURL, ";\x1b\x07") {
		return false
	}
	for _, r := range rawURL {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}

	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "github.com" || strings.HasSuffix(host, ".github.com")
}
