package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Access is what became of a read GitHub serves to only some of a
// repository's viewers — traffic needs push access (#73) — so the
// drill-in can tell the cases apart instead of rendering every one of
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
	return r
}

// ownerAccess decides what an owner-only read came to. err is what the
// fetch returned; viewerHasRole is what the detail query said about
// the viewer's role on the repository (push access for traffic).
//
// The token-specific refusal comes first, from GitHub's own wording:
// "Resource not accessible by personal access token" is the answer to
// a fine-grained token missing a permission, whatever the role. Then
// the role decides: a 403 or a 404 for a viewer without it is GitHub
// working as designed ("Must have push access to repository", measured
// on a repository the token can only read), while the same refusal for
// a viewer WITH the role can only be the token — a classic token
// without the scope, for instance.
func ownerAccess(err error, viewerHasRole bool) Access {
	if err == nil {
		return AccessOK
	}
	r, ok := err.(*refusal)
	if !ok || r.limited {
		return AccessFailed
	}
	if r.status != http.StatusForbidden && r.status != http.StatusNotFound {
		return AccessFailed
	}
	if strings.Contains(strings.ToLower(r.message), "resource not accessible") {
		return AccessTokenLacks
	}
	if viewerHasRole {
		return AccessTokenLacks
	}
	return AccessNotPermitted
}

// canPush reports whether a repository role carries push access, the
// role GitHub requires for traffic.
func canPush(permission string) bool {
	switch permission {
	case "ADMIN", "MAINTAIN", "WRITE":
		return true
	}
	return false
}
