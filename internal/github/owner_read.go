package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Access is what became of a read GitHub serves to only some of a
// repository's viewers — traffic needs push access (#73), Dependabot
// alerts an administrator (#58) — so the drill-in can tell the cases
// apart instead of rendering every one of
// them as an empty section. Only one of them is worth a sentence: a
// viewer who holds the role, with a token that does not carry it.
type Access int

const (
	// AccessNotAsked: no request was made — an unauthenticated
	// session, which GitHub would refuse anyway.
	AccessNotAsked Access = iota
	// AccessOK: the data arrived.
	AccessOK
	// AccessNotPermitted: GitHub keeps this for a role the viewer does
	// not hold on this repository. Nothing to explain — no page on
	// github.com shows it to them either.
	AccessNotPermitted
	// AccessTokenLacks: the viewer holds the role, but the token does
	// not carry the permission — a fine-grained token without it, or a
	// classic one without the scope.
	AccessTokenLacks
	// AccessDisabled: the feature is switched off for the repository —
	// "Dependabot alerts are disabled for this repository", measured on
	// a repository the viewer administers.
	AccessDisabled
	// AccessFailed: anything else — a 5xx, a rate limit, the network,
	// an unreadable body. The accompanying error says which.
	AccessFailed
)

// refusal is a non-2xx answer to an owner-only read, kept raw until
// the detail query has said whether the viewer holds the role the read
// needs: the same 403 means "not yours to see" for one viewer and "your
// token is short a permission" for another, and the status alone
// cannot tell them apart.
type refusal struct {
	status  int
	message string // GitHub's own "message", sanitized
	limited bool   // a rate limit, whatever the status says
}

func (r *refusal) Error() string {
	if r.message == "" {
		return fmt.Sprintf("GitHub answered %d", r.status)
	}
	return fmt.Sprintf("GitHub answered %d: %s", r.status, r.message)
}

// readRefusal turns a non-2xx response into a *refusal, reading
// GitHub's error message from the body. A rate limit is recognised
// from the headers first, because GitHub answers an exhausted budget
// with the same 403 it uses for a missing permission — the
// classifyStatus rule, applied here.
func readRefusal(resp *http.Response) *refusal {
	r := &refusal{status: resp.StatusCode}
	var body struct {
		Message string `json:"message"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if json.Unmarshal(raw, &body) == nil {
		r.message = Sanitize(strings.TrimSpace(body.Message))
	}
	switch reason := classifyStatus(resp.StatusCode, resp.Header); reason {
	case ReasonRateLimitPrimary, ReasonRateLimitSecondary:
		r.limited = true
	}
	// GitHub documents that a secondary limit can come back as a 403
	// with an explanatory message and no Retry-After, which the headers
	// alone would read as a refusal.
	if msg := strings.ToLower(r.message); strings.Contains(msg, "rate limit") || strings.Contains(msg, "abuse detection") {
		r.limited = true
	}
	return r
}

// ownerAccess decides what an owner-only read came to. err is what the
// fetch returned; viewerHasRole is what the detail query said about
// the viewer's role on the repository (push access for traffic).
//
// The role decides first. A 403 or 404 for a viewer without it is
// GitHub working as designed ("Must have push access to repository",
// measured on a repository the token can only read), and whatever the
// message says, nothing on screen may claim otherwise: a reader is not
// told "you can push here" because their token was also refused, nor
// shown a failure line for data they would never have been given.
//
// For a viewer WITH the role, a refusal is blamed on the token only
// when GitHub's own words say access is what is missing: "Resource not
// accessible by personal access token" (a fine-grained token short the
// permission), or the role refusal itself ("Must have push access",
// "You are not authorized to perform this operation") coming back to
// someone whose role has it, which
// leaves the token as the narrower of the two. Any other refusal — an
// organisation's SAML enforcement or IP allow list, a 404 — fails with
// GitHub's message, because "your token lacks the permission" would be
// the wrong fix to send someone to.
func ownerAccess(err error, viewerHasRole bool) Access {
	if err == nil {
		return AccessOK
	}
	// Without the role there is nothing to show whatever went wrong: a
	// reader whose request also hit a 502 would have been refused
	// anyway, and a failure line would be a section GitHub never shows
	// them.
	if !viewerHasRole {
		return AccessNotPermitted
	}
	r, ok := err.(*refusal)
	if !ok || r.limited {
		return AccessFailed
	}
	if r.status != http.StatusForbidden && r.status != http.StatusNotFound {
		return AccessFailed
	}
	msg := strings.ToLower(r.message)
	if r.status == http.StatusForbidden {
		// A feature switched off is a fact about the repository, said
		// as such, not a token to fix.
		if strings.Contains(msg, "alerts are disabled") {
			return AccessDisabled
		}
		for _, s := range []string{"resource not accessible", "must have push access", "you are not authorized to perform this operation"} {
			if strings.Contains(msg, s) {
				return AccessTokenLacks
			}
		}
	}
	return AccessFailed
}

// isAdmin reports whether a repository role administers it, the role
// GitHub requires for Dependabot alerts (an organisation can also grant
// them to security managers, which viewerPermission does not show: such
// a viewer with a token short the permission reads as not permitted).
func isAdmin(permission string) bool { return permission == "ADMIN" }

// canPush reports whether a repository role carries push access, the
// role GitHub requires for traffic.
func canPush(permission string) bool {
	switch permission {
	case "ADMIN", "MAINTAIN", "WRITE":
		return true
	}
	return false
}
