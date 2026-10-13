package github

import (
	"regexp"
	"strings"
	"testing"
)

// The dashboard snapshot's name (#65) separates "this token looking at
// this account" from every other pair, and carries nothing a token could
// be recovered from.
func TestCacheKeySeparatesTokenAndLogin(t *testing.T) {
	hex24 := regexp.MustCompile(`^[0-9a-f]{24}$`)
	base := cacheKey("ghp_tokenA", "")
	for _, k := range []string{base, cacheKey("", "octocat")} {
		if !hex24.MatchString(k) {
			t.Fatalf("key %q is not 24 hex digits", k)
		}
	}
	if cacheKey("ghp_tokenA", "") != base {
		t.Error("the same token and login gave two keys")
	}
	for name, other := range map[string]string{
		"another token":              cacheKey("ghp_tokenB", ""),
		"the same token, a login":    cacheKey("ghp_tokenA", "octocat"),
		"no token, the same login":   cacheKey("", "octocat"),
		"the token moved into login": cacheKey("", "ghp_tokenA"),
	} {
		if other == base {
			t.Errorf("%s shares the key %s", name, base)
		}
	}
	if cacheKey("t", "OctoCat") != cacheKey("t", "octocat") {
		t.Error("login case changed the key; GitHub logins are case-insensitive")
	}
	if strings.Contains(base, "tokenA") {
		t.Error("the key contains the token")
	}
}

// New derives the key from the token it actually resolved.
func TestNewDerivesTheCacheKeyFromItsToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_first")
	a, err := New("", Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Setenv("GITHUB_TOKEN", "ghp_second")
	b, err := New("", Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.CacheKey() != cacheKey("ghp_first", "") {
		t.Errorf("CacheKey() = %s, want the hash of the resolved token", a.CacheKey())
	}
	if a.CacheKey() == b.CacheKey() {
		t.Error("two tokens produced one cache key")
	}
}
