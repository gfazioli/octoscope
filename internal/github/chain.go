package github

// Axis 4 — capability escalation, the reusable-workflow chain (#106).
//
// parseWorkflow reads one file, which is the right shape for a parser and
// the wrong shape for this axis's question. The shape that slips through a
// per-file reading:
//
//	# caller — fork-triggered, holds no secret of its own
//	on: pull_request_target
//	jobs:
//	  call:
//	    uses: ./.github/workflows/reusable.yml
//	    secrets: inherit
//
//	# callee — reads a secret, but on its own `workflow_call` is not
//	# untrusted input
//	on: workflow_call
//	jobs:
//	  build:
//	    steps:
//	      - run: deploy --token ${{ secrets.DEPLOY_TOKEN }}
//
// Separately the caller holds nothing and the callee is not reachable by
// an outsider, so neither scores. Together they are a fork-triggered path
// to a repository secret. Composing them is what this file does.
//
// Two properties travel in opposite directions along a chain, and getting
// them the wrong way round is how a composition turns into a false
// positive:
//
//   - **Reachability accumulates.** If an outsider can start the caller,
//     an outsider can reach everything it calls, transitively.
//   - **Power is bounded by the giver.** A callee's `${{ secrets.X }}`
//     resolves to nothing unless the caller passed secrets, and its
//     permissions are the caller's — GitHub's reference is explicit that
//     what a callee receives "can be only downgraded (not elevated)". So a
//     callee declaring `contents: write` while its caller hands over
//     nothing holds nothing.
//
// Only local `./` calls are composed. Anything else — another repository,
// or this one addressed by its full name and a ref — is disclosed as an
// unfollowed chain rather than treated as absent.

import "sort"

// writeState is the elevated write reaching a workflow: the grants named,
// or the repository default when nothing was declared anywhere above it.
type writeState struct {
	Perms []string
	// InheritsDefault means the grant is whatever the repository setting
	// says, which only the probe in capability.go can answer (#107).
	InheritsDefault bool
}

// composed is one workflow's facts after the chain reaching it has been
// resolved. It supersedes the per-file facts wherever the two disagree.
type composed struct {
	// Triggers are the outsider-controlled events that can reach this
	// workflow, its own and any inherited from a caller.
	Triggers []string
	// ViaCallers names the callers that carried reachability in, so the
	// report can say *how* an outsider reaches a file whose own triggers
	// say they cannot.
	ViaCallers []string
	// Secrets reports whether the repository's secrets actually reach it.
	Secrets bool
	Write   writeState
	// CallerFound records that some local caller in this branch invokes
	// this workflow. Its absence is what stops a callee-only file from
	// being reported as if the scan knew who calls it.
	CallerFound bool
	// Unfollowed are `uses:` values that name a workflow this scan cannot
	// resolve — a different repository, or a ref it did not read.
	Unfollowed []string

	// ownSecretRefs is the file's own answer to "does anything here reach a
	// secret" — a `${{ secrets.X }}` reference, or a `secrets: inherit`
	// that forwards the whole set onward. Kept even for a callee-only file,
	// where it is not an answer on its own but is exactly the half a
	// passing caller completes: a reference resolves to nothing until
	// somebody hands the secrets over.
	ownSecretRefs bool
}

// composeBranchChains composes each branch's chains and merges the results
// by path.
//
// Composition is per branch because a side branch can wire the same files
// together differently — which is the divergence this scan exists to catch,
// so it must not be flattened away before composing. The merge afterwards
// is a union: a chain that exists on *any* branch is a real path to that
// file, and the scoring loop already treats a workflow present on many
// branches as one fact about the repository rather than one per branch.
func composeBranchChains(branches []scanBranch, blobs map[string]blobAnalysis) map[string]*composed {
	merged := map[string]*composed{}
	for _, b := range branches {
		index := map[string]*workflowFacts{}
		for _, m := range b.Matches {
			if m.Rule.Class != classCI {
				continue
			}
			if ba, ok := blobs[m.BlobSHA]; ok && ba.Workflow != nil && !ba.Workflow.Unparsed {
				index[m.Path] = ba.Workflow
			}
		}
		for path, c := range composeChain(index) {
			into, ok := merged[path]
			if !ok {
				merged[path] = c
				continue
			}
			into.Triggers = union(into.Triggers, c.Triggers)
			into.ViaCallers = union(into.ViaCallers, c.ViaCallers)
			into.Unfollowed = union(into.Unfollowed, c.Unfollowed)
			into.Write.Perms = union(into.Write.Perms, c.Write.Perms)
			into.Secrets = into.Secrets || c.Secrets
			into.Write.InheritsDefault = into.Write.InheritsDefault || c.Write.InheritsDefault
			into.CallerFound = into.CallerFound || c.CallerFound
		}
	}
	return merged
}

func union(a, b []string) []string {
	for _, s := range b {
		if !contains(a, s) {
			a = append(a, s)
		}
	}
	sort.Strings(a)
	return a
}

// composeChain resolves the local reusable-workflow chains within one
// branch and returns the composed facts per workflow path.
//
// index maps a repository-relative workflow path to the facts parsed from
// it. Callers that are not in the index cannot be followed; callees that
// are not either are recorded as unfollowed by nobody, since a `./` path
// pointing at a file that is not there resolves to nothing at run time too.
func composeChain(index map[string]*workflowFacts) map[string]*composed {
	out := make(map[string]*composed, len(index))
	for path, f := range index {
		c := &composed{
			Triggers:      append([]string(nil), f.OutsiderTriggers...),
			Unfollowed:    unfollowedTargets(f),
			ownSecretRefs: f.UsesSecrets,
		}
		// A file nothing but another workflow can start has no power and no
		// exposure of its own to report — both arrive from its callers, and
		// asserting either from the file alone is the bug this fixes. Every
		// other file stands on its own facts.
		if !f.CallableOnly {
			c.Secrets = f.UsesSecrets
			c.Write = writeState{Perms: f.WritePerms, InheritsDefault: f.InheritsDefaultPerms}
		}
		out[path] = c
	}

	// Propagate to a fixpoint rather than recursing, which makes a cycle
	// (A calls B calls A) terminate by construction instead of needing a
	// visited set threaded through. One round can extend a chain by one
	// hop, so len(index) rounds is the most that can be needed; the loop
	// exits as soon as a round changes nothing, which is the normal case
	// after two.
	for round := 0; round < len(index); round++ {
		changed := false
		// Deterministic order: these merges append to slices that end up in
		// report text, and Go randomises map iteration.
		for _, callerPath := range sortedKeys(index) {
			caller := index[callerPath]
			for _, call := range caller.Calls {
				callee, ok := out[call.Path]
				if call.Path == "" || !ok {
					continue // unfollowable, or points at nothing in this branch
				}
				if mergeCall(out[callerPath], caller, callee, call, callerPath) {
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	for _, c := range out {
		sort.Strings(c.Triggers)
		sort.Strings(c.ViaCallers)
		sort.Strings(c.Unfollowed)
		sort.Strings(c.Write.Perms)
	}
	return out
}

// mergeCall folds one call edge into the callee's composed facts and
// reports whether anything changed, which is what drives the fixpoint.
func mergeCall(callerComposed *composed, caller *workflowFacts, callee *composed, call workflowCall, callerPath string) bool {
	changed := false

	// Reachability accumulates: whatever can start the caller can reach
	// the callee.
	for _, t := range callerComposed.Triggers {
		if !contains(callee.Triggers, t) {
			callee.Triggers = append(callee.Triggers, t)
			changed = true
		}
	}
	if len(callerComposed.Triggers) > 0 && !contains(callee.ViaCallers, callerPath) {
		callee.ViaCallers = append(callee.ViaCallers, callerPath)
		changed = true
	}
	if !callee.CallerFound {
		callee.CallerFound = true
		changed = true
	}

	// Power is bounded by the giver. The callee's own references only
	// resolve to something if this caller handed the secrets over.
	if call.PassesSecrets && callee.ownSecretRefs && !callee.Secrets {
		callee.Secrets = true
		changed = true
	}

	for _, p := range grantReaching(callerComposed, caller, call) {
		if !contains(callee.Write.Perms, p) {
			callee.Write.Perms = append(callee.Write.Perms, p)
			changed = true
		}
	}
	if inheritsReaching(callerComposed, caller, call) && !callee.Write.InheritsDefault {
		callee.Write.InheritsDefault = true
		changed = true
	}
	return changed
}

// grantReaching is the elevated write this call confers on its callee.
//
// A calling job that declares grants confers those. One that declares a
// block granting nothing elevated — `permissions: {}`, `contents: read` —
// confers nothing, which is the case a per-file reading gets wrong in the
// dangerous direction. One that declares nothing passes on whatever
// reached *it*, which for a callee-only caller is what its own callers
// gave it rather than the repository default.
func grantReaching(callerComposed *composed, caller *workflowFacts, call workflowCall) []string {
	if len(call.WritePerms) > 0 {
		return call.WritePerms
	}
	if call.InheritsDefaultPerms && caller.CallableOnly {
		return callerComposed.Write.Perms
	}
	return nil
}

// inheritsReaching reports whether what reaches the callee is the
// repository default rather than a named grant — the half only the #107
// probe can resolve.
func inheritsReaching(callerComposed *composed, caller *workflowFacts, call workflowCall) bool {
	if !call.InheritsDefaultPerms {
		return false
	}
	if caller.CallableOnly {
		// The caller is itself a callee: it declared nothing, so it passes
		// on what it received, which is the repository default only if that
		// is what reached it.
		return callerComposed.Write.InheritsDefault
	}
	return true
}

// unfollowedTargets lists the `uses:` values naming a workflow that could
// not be resolved locally.
func unfollowedTargets(f *workflowFacts) []string {
	var out []string
	for _, c := range f.Calls {
		if c.Remote != "" {
			out = append(out, c.Remote)
		}
	}
	return out
}

func sortedKeys(m map[string]*workflowFacts) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
