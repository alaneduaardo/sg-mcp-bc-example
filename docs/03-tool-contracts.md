# Tool Contracts — MCP for Batch Changes (v1)

**Scope rule:** v1 composes, validates and previews. **v1 never executes or publishes.** Human approval is an invariant, not a feature flag.

**Public-API boundary (declared everywhere it applies):** target resolution and file reading run for real against the public Sourcegraph instance; step execution and publication are Enterprise surfaces — `bc_request_publish` ships as a documented contract, not an implementation.

---

## 1. `bc_find_targets` — discovery (the `on:` clause factory)

Purpose: turn a search query into batch-change targeting. Output is shaped for spec composition, not for browsing — this is deliberately *not* a generic search tool (the official MCP already exposes those; duplicating them would be incoherent).

```json
{
  "input": {
    "query":     {"type": "string", "desc": "Sourcegraph search syntax"},
    "max_repos": {"type": "integer", "default": 25, "max": 100}
  },
  "output": {
    "targets": [{
      "repo": "string",
      "occurrence_count": "integer",
      "sample_paths": ["string (≤5)"]
    }],
    "normalized_query": {"type": "string",
      "desc": "ready to drop into on.repositoriesMatchingQuery"},
    "total_repos": "integer",
    "truncated": "boolean"
  },
  "errors": ["INVALID_QUERY (with reason)", "UPSTREAM_UNAVAILABLE"]
}
```

Token economics: fragments only (counts + sample paths). Full content is `bc_inspect_target`'s job — discovery is broad and cheap; inspection is narrow and deep.

## 2. `bc_inspect_target` — inspection (grounding the steps)

Purpose: full file content, in the context of an identified target, so the agent can ground the transformation it will propose. Separated from discovery to avoid context explosion and to mirror how agents actually work (search, then read).

```json
{
  "input": {
    "repo": "string",
    "path": "string",
    "rev":  {"type": "string", "optional": true, "default": "HEAD"}
  },
  "output": {
    "content": "string",
    "rev_resolved": "string",
    "size_bytes": "integer"
  },
  "errors": ["NOT_FOUND", "TOO_LARGE (limit stated)", "UPSTREAM_UNAVAILABLE"]
}
```

## 3. `bc_create_spec` — composition (pure; never executes)

Purpose: assemble and validate the existing declarative batch spec format from the conversation. The agent composes; the artifact stays reviewable by humans before anything else can happen. The structured YAML is the guardrail, not the limitation.

```json
{
  "input": {
    "name": "string (spec name rules enforced)",
    "description": "string",
    "on": {"repositoriesMatchingQuery": "string  // from bc_find_targets.normalized_query"},
    "steps": [{
      "run": "string (container command — deterministic steps only in v1)",
      "container": "string (image)"
    }],
    "changeset_template": {
      "title": "string", "body": "string",
      "branch": "string (branch-name rules enforced)",
      "commit": {"message": "string"}
    }
  },
  "output": {
    "spec_yaml": "string (canonical YAML)",
    "valid": "boolean",
    "warnings": ["string  // e.g. broad query, missing body, suspicious step"]
  },
  "errors": ["VALIDATION_FAILED (field-level detail)"]
}
```

v1 restriction, stated in the schema description: deterministic steps only. Agent-generated steps are a v2 question gated on the measurement layer (analysis doc §5).

## 4. `bc_preview` — dry run (the local half of it)

Purpose: resolve what the spec *would* touch, without touching anything.

```json
{
  "input": {"spec_yaml": "string"},
  "output": {
    "resolved_repos": ["string"],
    "estimated_changesets": "integer",
    "validation": {"valid": "boolean", "issues": ["string"]},
    "boundary_note": "constant string: target resolution via public API; step
                      execution requires Enterprise executors and is out of scope"
  },
  "errors": ["INVALID_SPEC", "UPSTREAM_UNAVAILABLE"]
}
```

## 5. `bc_request_publish` — contract only (the governance statement)

Purpose in v1: encode, as a schema, what governed publication would require. Calling it returns `NOT_IMPLEMENTED` plus the documented semantics — the contract *is* the deliverable.

```json
{
  "input": {
    "spec_yaml": "string",
    "approval": {
      "approver": "string (human identity — required, no agent self-approval)",
      "token": "string (out-of-band approval token)"
    },
    "rollout": {
      "mode": {"enum": ["staged"], "default": "staged"},
      "initial_batch": {"type": "integer", "default": 5},
      "halt_on_failure_rate": {"type": "number", "default": 0.2}
    }
  },
  "output": {"status": "NOT_IMPLEMENTED", "semantics_doc": "string (this section)"},
  "governance_semantics": {
    "scope": "dedicated write scope, separate from MCP read scope — 'propose but not publish' must be expressible",
    "audit": "every action attributable: which agent, authorized by which human, when, what",
    "default": "dry-run; publication requires explicit human approval — invariant",
    "v2_gate": "graduates only when the measurement layer exists (blast radius scoring, CI-signal tiering, canary stop rule) — without risk-tiering, human approval of bespoke diffs at scale is theater"
  }
}
```

---

## Demo narrative (cycle 3 target)

Claude Code connected to this server:
1. *"Find everywhere we use pattern X"* → `bc_find_targets`
2. *"Show me the context in repo Y"* → `bc_inspect_target`
3. *"Propose the fix as a batch change"* → `bc_create_spec` (the `on:` clause derived from step 1) → validated YAML
4. `bc_preview` → resolved repos, estimated changesets
5. `bc_request_publish` → NOT_IMPLEMENTED + governance semantics — the proposal's thesis, demonstrated by refusal.
