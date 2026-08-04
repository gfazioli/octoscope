package github

// Axis 4 — capability escalation, the workflow half.
//
// This file only *extracts facts* from a workflow file; the scoring
// lives in evaluateScan with the other axes, so the same pure-function
// discipline applies.
//
// The distinction that makes this axis useful is not "does the workflow
// hold power" but "is that power reachable from untrusted input".
// octoscope's own release.yml requests contents:write and reads two
// secrets, and it is entirely correct: it triggers on a tag push, which
// only someone who can already push tags can cause. The same file
// triggered by pull_request_target would be a privilege-escalation
// gadget. Scoring capability alone would light up a large share of
// GitHub, teaching everyone to ignore the axis.

import (
	"regexp"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// forkTriggers are the events that run with the *base* repository's
// token and secrets while acting on input an outsider controls. They
// are the reason capability matters.
//
// pull_request is deliberately absent: a fork PR on that event gets a
// read-only token and no secrets, which is the safe design.
var forkTriggers = map[string]string{
	"pull_request_target": "runs with the base repo's token and secrets against a fork's pull request",
	"workflow_run":        "runs in the base repo context after another workflow, with secrets available",
	"issue_comment":       "fires on a comment from anyone who can comment",
}

// workflowFacts is what one workflow file tells us about capability.
type workflowFacts struct {
	// ForkTriggers are the untrusted-input events this workflow answers.
	ForkTriggers []string
	// WritePerms are the elevated grants found, top-level or per-job —
	// "write-all", "contents: write", "id-token: write", …
	WritePerms []string
	// UsesSecrets reports whether the file can reach the secrets
	// context — by reference, or by handing them to a called workflow.
	UsesSecrets bool
	// SelfHostedJobs is true when any job targets a self-hosted runner,
	// which is what makes an attached runner actually *reachable*.
	SelfHostedJobs bool
	// Unparsed is true when the content could not be decoded as YAML, so
	// the report can say the file was not understood rather than imply
	// it was checked and found clean.
	Unparsed bool
}

// parseWorkflow extracts the capability facts from one workflow file.
//
// Callers must cap the content length before calling — the scan already
// does, via maxBlobScanBytes — because this hands attacker-controlled
// bytes to a YAML parser.
func parseWorkflow(content []byte) workflowFacts {
	var f workflowFacts
	// Secrets are detected in two halves, because they arrive in two
	// genuinely different shapes.
	//
	// The textual half stays textual on purpose: a reference can hide in a
	// script body, an env block or a with: input, and all of them are just
	// strings by the time YAML is done. What it looks for is the context
	// *named inside an expression*, not a fixed spelling — `secrets.NAME`
	// and `secrets['NAME']` are the common forms, but `toJSON(secrets)`
	// serialises the whole set while containing neither, and a check for
	// "secrets." alone waves it through.
	//
	// The structural half is below, after the decode: `secrets: inherit`
	// is a YAML mapping, so reading it out of the bytes made the answer
	// depend on spelling YAML does not care about.
	text := string(content)
	f.UsesSecrets = mentionsSecretsContext(text) ||
		// Kept as a fallback for the unparsable case only: the structural
		// read below cannot run when the decode fails, and a file we
		// could not parse is exactly where guessing less is worse.
		strings.Contains(text, "secrets: inherit")

	var root map[string]any
	if err := yaml.Unmarshal(content, &root); err != nil || root == nil {
		f.Unparsed = true
		return f
	}

	// YAML 1.1 treats a bare `on` as a boolean, which is a real trap in
	// GitHub Actions files — but *not* with this decoder target.
	// Measured against yaml.v3: unmarshalling "on: push" into
	// map[string]any yields the key "on", and only a typed target such
	// as map[bool]any produces map[true:push]. The "true" lookup is
	// kept as cheap insurance in case the decode target or the library
	// ever changes, not because it fires today.
	trigger := root["on"]
	if trigger == nil {
		trigger = root["true"]
	}
	for _, ev := range eventNames(trigger) {
		if _, ok := forkTriggers[ev]; ok {
			f.ForkTriggers = append(f.ForkTriggers, ev)
		}
	}

	f.WritePerms = append(f.WritePerms, writeGrants(root["permissions"])...)
	// Per-job permissions override the top level, so both matter.
	if jobs, ok := root["jobs"].(map[string]any); ok {
		for _, j := range jobs {
			job, ok := j.(map[string]any)
			if !ok {
				continue
			}
			f.WritePerms = append(f.WritePerms, writeGrants(job["permissions"])...)
			if runsOnSelfHosted(job["runs-on"]) {
				f.SelfHostedJobs = true
			}
			// The structural half of the secrets check. A reusable-workflow
			// call hands over the whole set with `secrets: inherit`, and
			// that is a mapping: `"secrets": inherit`, `'secrets': inherit`
			// and `secrets:    inherit` all decode to exactly what GitHub
			// Actions consumes, while none of them contain the substring
			// "secrets: inherit". Reading the decoded value is spelling
			// agnostic by construction.
			if s, ok := job["secrets"].(string); ok && strings.TrimSpace(s) == "inherit" {
				f.UsesSecrets = true
			}
		}
	}
	f.WritePerms = dedupeStrings(f.WritePerms)
	return f
}

// actionsExpr matches a single `${{ … }}` expression. The capture is
// non-greedy so it stops at the first closing braces: a greedy match would
// span from one expression to a later one and report the context as
// mentioned in between, where it is not.
var actionsExpr = regexp.MustCompile(`(?s)\$\{\{(.*?)\}\}`)

// secretsIdentifier matches the secrets context named as a whole word, so
// `toJSON(secrets)` and a bare `secrets` both count while `mysecrets` or
// `secretsmanager` do not.
var secretsIdentifier = regexp.MustCompile(`\bsecrets\b`)

// mentionsSecretsContext reports whether any Actions expression in the
// file names the secrets context. Every path to a secret value runs
// through an expression — a script body, an env value, a with: input — so
// looking inside the expressions catches the named forms and the
// serialising ones alike, and looking *only* inside them is what keeps
// the word "secrets" in a comment or a step name from scoring.
func mentionsSecretsContext(text string) bool {
	for _, m := range actionsExpr.FindAllStringSubmatch(text, -1) {
		if secretsIdentifier.MatchString(m[1]) {
			return true
		}
	}
	return false
}

// eventNames normalises the three shapes `on:` can take — a bare string,
// a list, or a mapping of event to filters.
func eventNames(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case map[string]any:
		var out []string
		for k := range t {
			out = append(out, k)
		}
		// Go randomises map iteration, and these names are joined into
		// report text: unsorted, two scans of the same repository would
		// render different sentences. The delta section already sorts
		// for this reason.
		sort.Strings(out)
		return out
	}
	return nil
}

// writeGrants normalises a `permissions:` value into the elevated grants
// it confers. Read-only scopes are not returned: they are the safe
// default and listing them would drown the signal.
func writeGrants(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "write-all" {
			return []string{"write-all"}
		}
	case map[string]any:
		var out []string
		for scope, val := range t {
			s, ok := val.(string)
			if !ok || s != "write" {
				continue
			}
			out = append(out, scope+": write")
		}
		sort.Strings(out) // deterministic report text; see eventNames
		return out
	}
	return nil
}

// runsOnSelfHosted reports whether a job's runs-on targets a
// self-hosted runner. The value may be a string, a list of labels, or a
// group/labels mapping; "self-hosted" is the conventional label, and a
// group assignment is self-hosted by definition.
func runsOnSelfHosted(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.Contains(t, "self-hosted")
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && strings.Contains(s, "self-hosted") {
				return true
			}
		}
	case map[string]any:
		if _, ok := t["group"]; ok {
			return true
		}
		return runsOnSelfHosted(t["labels"])
	}
	return false
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
