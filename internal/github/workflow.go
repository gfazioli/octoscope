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

// outsiderTriggers are the events that run with the *base* repository's
// token and secrets while acting on input an outsider controls. They
// are the reason capability matters.
//
// The list is about *who can cause the run*, not about forks — which is
// why the name is not forkTriggers, and why `issues` belongs here (#111):
// on a public repository anyone can open one, the title and body are
// theirs, and the run gets the base repository's token and secrets.
// `issue_comment` was already listed on exactly that reasoning, and opening
// an issue cannot be less untrusted than commenting on one.
//
// pull_request is deliberately absent, **on a public repository**: a fork
// PR on that event gets a read-only token and no secrets. The qualifier is
// deliberate — whether the private-repository and organisation fork
// policies can lift that is an open question, not a settled fact, and it is
// tracked in #114 rather than assumed either way here.
//
// Events left out for now, and why they are a question rather than an
// omission: `discussion`, `discussion_comment`, `fork` and `watch` are
// publicly triggerable only when the corresponding repository feature is
// enabled, which the scan has no input for — adding them blind would score
// workflows an outsider cannot reach, and on this axis a wrong positive is
// what teaches everyone to ignore it. Also #114.
var outsiderTriggers = map[string]string{
	"pull_request_target": "runs with the base repo's token and secrets against a fork's pull request",
	"workflow_run":        "runs in the base repo context after another workflow, with secrets available",
	"issue_comment":       "fires on a comment from anyone who can comment",
	"issues":              "fires on an issue anyone can open, carrying their title and body",
}

// workflowCall is one reusable-workflow invocation — a job whose `uses:`
// names another workflow rather than running steps.
//
// It carries what the *calling job* hands over, because that is what
// decides the callee's power and the callee's own file cannot show it:
// GitHub's reference is explicit that "if jobs.<job_id>.permissions is
// not specified in the calling job, the called workflow will have the
// default permissions for the GITHUB_TOKEN", and that what a callee
// receives "can be only downgraded (not elevated)". So the callee is
// scored on the caller's grant, reduced by its own if it declares one.
type workflowCall struct {
	// Path is the callee, repository-relative, resolved from the `./`
	// form GitHub requires for a workflow in the same repository. Empty
	// when the target is not local — see Remote.
	Path string
	// Remote is the raw `uses:` value when it is not a local `./` path:
	// another repository, or this one addressed by its full name and a
	// ref. Both are followed by nobody here, so the chain is *disclosed*
	// rather than silently treated as absent (#106 scopes the cross-repo
	// case out; pretending it was checked is the part that would be
	// wrong).
	Remote string
	// PassesSecrets is true when the calling job hands the repository's
	// secrets over — `secrets: inherit`, or a by-name mapping whose values
	// reference the secrets context. It matters because without it a
	// callee's own `${{ secrets.X }}` resolves to nothing: the reference
	// is there, the secret is not.
	PassesSecrets bool
	// WritePerms are the elevated grants the calling job confers.
	WritePerms []string
	// InheritsDefaultPerms is true when neither the calling job nor its
	// workflow declares a `permissions:` block, so what reaches the callee
	// is the repository default (#107).
	InheritsDefaultPerms bool
}

// workflowFacts is what one workflow file tells us about capability.
type workflowFacts struct {
	// OutsiderTriggers are the untrusted-input events this workflow answers.
	OutsiderTriggers []string
	// WritePerms are the elevated grants found, top-level or per-job —
	// "write-all", "contents: write", "id-token: write", …
	WritePerms []string
	// UsesSecrets reports whether the file can reach the secrets
	// context — by reference, or by handing them to a called workflow.
	UsesSecrets bool
	// SelfHostedJobs is true when any job targets a self-hosted runner,
	// which is what makes an attached runner actually *reachable*.
	SelfHostedJobs bool
	// Calls are the reusable workflows this file invokes, one per calling
	// job. Composing caller and callee is what turns two innocent-looking
	// files into a scored path (#106); the composition itself lives in the
	// scoring engine, which is the only place that can see the other files.
	Calls []workflowCall
	// CallableOnly is true when `workflow_call` is this file's only
	// trigger, i.e. nothing reaches it except another workflow. Its
	// permissions and its reachability are then both its callers' to
	// decide, so claiming either from the file alone is wrong.
	CallableOnly bool
	// InheritsDefaultPerms is true when at least one job would run with
	// the *repository's* default permissions instead of declared ones,
	// because neither the workflow nor that job declares a `permissions:`
	// block. What that default is is not in the file — it is a repository
	// setting an owner can widen to read/write — so resolving it needs the
	// probe in capability.go (#107). A workflow that declares anything,
	// even `contents: read`, overrides the default entirely and is not
	// affected.
	InheritsDefaultPerms bool
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

	var root map[string]any
	if err := yaml.Unmarshal(content, &root); err != nil || root == nil {
		f.Unparsed = true
		// Nothing decoded to walk, and a file we could not parse is the
		// worst place to claim it holds nothing — so fall back to the raw
		// bytes here, accepting that a commented-out reference counts.
		text := string(content)
		f.UsesSecrets = mentionsSecretsContext(text) ||
			strings.Contains(text, "secrets: inherit")
		return f
	}

	// Secrets arrive in two genuinely different shapes, so they are
	// detected two ways.
	//
	// References are expressions living inside scalar values — a script
	// body, an env value, a with: input — which are opaque to YAML but are
	// still scalars, so walking the decoded document reaches all of them
	// while skipping what YAML discards. A commented-out reference is not
	// a scalar, and a workflow whose only mention is disabled reaches
	// nothing.
	//
	// `secrets: inherit` is the other shape, read structurally below: it
	// is a mapping, and reading it out of the bytes made the answer depend
	// on spelling YAML does not care about.
	walkScalars(root, func(s string) {
		if !f.UsesSecrets && mentionsSecretsContext(s) {
			f.UsesSecrets = true
		}
	})

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
	events := eventNames(trigger)
	for _, ev := range events {
		if _, ok := outsiderTriggers[ev]; ok {
			f.OutsiderTriggers = append(f.OutsiderTriggers, ev)
		}
	}
	// Nothing but another workflow can start this file, so both its power
	// and its exposure are decided elsewhere (#106).
	f.CallableOnly = len(events) == 1 && events[0] == "workflow_call"

	f.WritePerms = append(f.WritePerms, writeGrants(root["permissions"])...)
	// Whether the *top level* declares a block at all, which is a
	// different question from whether it grants anything: `permissions:
	// {contents: read}` grants nothing elevated but does override the
	// repository default, so it must not read as inheriting.
	_, topDeclaresPerms := root["permissions"]

	// Per-job permissions override the top level, so both matter.
	if jobs, ok := root["jobs"].(map[string]any); ok {
		for _, j := range jobs {
			job, ok := j.(map[string]any)
			if !ok {
				continue
			}
			// A job inherits the repository default only when nothing
			// above it declared one either. One such job is enough: the
			// workflow can hold power the file does not mention.
			if !topDeclaresPerms {
				if _, jobDeclaresPerms := job["permissions"]; !jobDeclaresPerms {
					f.InheritsDefaultPerms = true
				}
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

			// A job whose `uses:` names a workflow is a call, not steps.
			// Only the job level counts: a step's `uses:` is an action, and
			// treating the two alike would read every actions/checkout as a
			// reusable-workflow chain.
			if uses, ok := job["uses"].(string); ok {
				if call, ok := parseWorkflowCall(uses); ok {
					call.PassesSecrets = passesSecrets(job["secrets"])
					// What the callee receives is the calling job's grant,
					// falling back to the workflow's, and to the repository
					// default only when neither declares anything.
					if _, jobDeclares := job["permissions"]; jobDeclares {
						call.WritePerms = writeGrants(job["permissions"])
					} else if topDeclaresPerms {
						call.WritePerms = writeGrants(root["permissions"])
					} else {
						call.InheritsDefaultPerms = true
					}
					f.Calls = append(f.Calls, call)
				}
			}
		}
		// Go randomises map iteration and these drive report text; see
		// eventNames.
		sort.Slice(f.Calls, func(i, j int) bool {
			if f.Calls[i].Path != f.Calls[j].Path {
				return f.Calls[i].Path < f.Calls[j].Path
			}
			return f.Calls[i].Remote < f.Calls[j].Remote
		})
	}
	f.WritePerms = dedupeStrings(f.WritePerms)
	return f
}

// secretsRootRef matches the secrets context at the *root* of a
// reference: `secrets.NAME`, `secrets['NAME']`, `toJSON(secrets)` and a
// bare `secrets` all qualify. Requiring no preceding word character or
// dot is what excludes `vars.secrets` — a configuration variable that
// merely happens to be called secrets reaches nothing — as well as
// `mysecrets` and `secretsmanager`.
var secretsRootRef = regexp.MustCompile(`(^|[^\w.])secrets($|[^\w])`)

// expressionCode returns the code of every `${{ … }}` expression in s,
// with the contents of quoted literals omitted.
//
// Tracking quote state earns its keep twice over. A literal's contents are
// data, not a reference, so `contains(msg, 'secrets')` — a workflow
// reacting to the *word* — must not count. And a `}}` inside a literal
// does not end the expression: Actions escapes braces for format() by
// doubling them, so `format('{{Hello {0}!}}', secrets.TOKEN)` carries a
// `}}` mid-literal, and stopping there would hide the reference that
// follows it.
func expressionCode(s string) []string {
	var out []string
	for {
		open := strings.Index(s, "${{")
		if open < 0 {
			return out
		}
		s = s[open+3:]
		var code strings.Builder
		quote := byte(0)
		i := 0
		for i < len(s) {
			c := s[i]
			switch {
			case quote != 0:
				// A doubled quote is an escaped one: stay inside.
				if c == quote {
					if i+1 < len(s) && s[i+1] == quote {
						i += 2
						continue
					}
					quote = 0
				}
				i++
			case c == '\'' || c == '"':
				// Actions uses single quotes; double quotes are not valid
				// there, but treating them as a literal too keeps a
				// malformed file from scoring on plain text.
				quote = c
				i++
			case c == '}' && i+1 < len(s) && s[i+1] == '}':
				i += 2
				goto done
			default:
				code.WriteByte(c)
				i++
			}
		}
	done:
		out = append(out, code.String())
		s = s[i:]
	}
}

// parseWorkflowCall classifies a job-level `uses:` value.
//
// GitHub requires the `./`-prefixed path for a workflow in the same
// repository — `uses: ./.github/workflows/workflow-2.yml`, no ref — while
// another repository is `owner/repo/.github/workflows/x.yml@ref`. Only the
// first can be resolved against the tree this scan already walked.
//
// Everything else that still points at a workflow file is returned as
// Remote rather than dropped, including *this* repository addressed by its
// full name and a ref: that spelling is a same-repository call the scan
// cannot follow, since the ref may not be a branch it read. Reporting it
// as an unfollowed chain is the honest answer; silently ignoring it would
// make the more obscure spelling the one that evades composition.
func parseWorkflowCall(uses string) (workflowCall, bool) {
	u := strings.TrimSpace(uses)
	if u == "" {
		return workflowCall{}, false
	}
	if rest, ok := strings.CutPrefix(u, "./"); ok {
		// A local call carries no ref; if one is present the file is not
		// what GitHub would resolve either, so treat it as unfollowable.
		if strings.Contains(rest, "@") {
			return workflowCall{Remote: u}, true
		}
		return workflowCall{Path: rest}, true
	}
	// A workflow call always names a workflow file. A step-level action
	// (actions/checkout@v4) never does, which is the discriminator that
	// keeps composite actions out of this — they are a different problem
	// and run under their caller's token anyway.
	if strings.Contains(u, ".github/workflows/") {
		return workflowCall{Remote: u}, true
	}
	return workflowCall{}, false
}

// passesSecrets reports whether a calling job hands the repository's
// secrets to the workflow it calls. Two shapes count: the `inherit`
// keyword, and a by-name mapping whose values reference the secrets
// context (`secrets: {token: ${{ secrets.GITHUB_TOKEN }}}`).
//
// A mapping whose values reference something else — an input, a var, a
// literal — passes no secret, and counting it would score a callee that
// receives nothing sensitive.
func passesSecrets(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t) == "inherit"
	case map[string]any:
		for _, val := range t {
			s, ok := val.(string)
			if ok && mentionsSecretsContext(s) {
				return true
			}
		}
	}
	return false
}

// mentionsSecretsContext reports whether any Actions expression in s names
// the secrets context. Callers pass decoded YAML scalars rather than the
// whole file: every path to a secret value runs through an expression in
// some scalar — a script body, an env value, a with: input — while a
// commented-out `# ${{ secrets.TOKEN }}` is not a scalar at all, and a
// workflow whose only reference is disabled reaches nothing.
func mentionsSecretsContext(s string) bool {
	for _, code := range expressionCode(s) {
		if secretsRootRef.MatchString(code) {
			return true
		}
	}
	return false
}

// walkScalars visits every scalar string in a decoded YAML value. Keys are
// skipped: a key is a name, not a place an expression is evaluated.
func walkScalars(v any, visit func(string)) {
	switch t := v.(type) {
	case string:
		visit(t)
	case []any:
		for _, e := range t {
			walkScalars(e, visit)
		}
	case map[string]any:
		for _, e := range t {
			walkScalars(e, visit)
		}
	}
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
