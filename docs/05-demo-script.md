# Demo Script — driving the five tools end to end

A **reproducible** walkthrough of the server: the prompts to give the agent, the
tool calls each prompt should provoke, and the shape of what comes back. It
doubles as a recording script (see [Recording checklist](#recording-checklist)).

Everything here runs **dry-run, against the public Sourcegraph instance**.
Nothing publishes; `bc_request_publish` refuses by contract. The only thing the
demo touches in the outside world is read traffic to `sourcegraph.com`.

> **Reproducibility caveat.** The queries below hit the live public index, so
> exact repo names, counts and `truncated` flags drift over time. The *shape* of
> every response is stable; the *numbers* are illustrative. Where a scenario
> depends on a precise repo or file path, it is called out — pick a current one
> if the named one has moved.

---

## Setup (once)

```sh
go build -o /tmp/bc-server ./mcp/bc/cmd/server
```

Point an MCP client (Claude Code shown) at the binary over stdio, passing the
endpoint through its `env` (the binary reads only the environment — see the
README):

```json
{
  "mcpServers": {
    "bc": {
      "command": "/tmp/bc-server",
      "env": { "SG_BASE_URL": "https://sourcegraph.com/.api/graphql" }
    }
  }
}
```

Confirm the five tools are listed: `bc_find_targets`, `bc_inspect_target`,
`bc_create_spec`, `bc_preview`, `bc_request_publish`.

**How to read each scenario.** *Developer* is what you type to the agent;
*Agent → tool* is the call the agent should make (arguments match the schemas in
[`03-tool-contracts.md`](03-tool-contracts.md)); *Result* is the abbreviated tool
output. JSON is trimmed for readability — only the fields that carry the point
are shown.

---

## Scenario 1 — Single query, end to end

**Goal:** the whole arc from one search query to the governed refusal — the
canonical demo. Theme: replace the deprecated `ioutil.ReadFile` with
`os.ReadFile` (Go 1.16+), a deterministic codemod.

**1. Discover.**

> **Developer:** *Find Go code in the Sourcegraph org that still calls
> `ioutil.ReadFile`.*

```
Agent → bc_find_targets {
  "queries": ["repo:^github\\.com/sourcegraph/ lang:go ioutil.ReadFile"]
}
```
```jsonc
Result {
  "targets": [
    { "repo": "github.com/sourcegraph/sourcegraph", "occurrence_count": 12,
      "sample_paths": ["internal/.../a.go", "cmd/.../b.go"] },
    { "repo": "github.com/sourcegraph/src-cli", "occurrence_count": 3, "sample_paths": [...] }
  ],
  "normalized_queries": ["repo:^github\\.com/sourcegraph/ lang:go ioutil.ReadFile"],
  "total_repos": 7,
  "truncated": false
}
```

The `normalized_queries[0]` value is what feeds the spec's `on:` clause in step 3
— discovery output is shaped to drop straight into composition.

**2. Inspect** (ground the transformation in a real file).

> **Developer:** *Show me one of the call sites.*

```
Agent → bc_inspect_target {
  "repo": "github.com/sourcegraph/sourcegraph",
  "path": "<a path from sample_paths above>"
}
```
```jsonc
Result { "content": "package ...\n...ioutil.ReadFile(...)...", "rev_resolved": "<40-char SHA>", "size_bytes": 1843 }
```

**3. Compose** (the agent assembles the spec; the `on:` query is the normalized
query from step 1).

> **Developer:** *Propose a batch change that replaces it with `os.ReadFile`.*

```
Agent → bc_create_spec {
  "name": "ioutil-to-os",
  "description": "Replace deprecated ioutil.ReadFile with os.ReadFile (Go 1.16+)",
  "on": { "repositoriesMatchingQuery": "repo:^github\\.com/sourcegraph/ lang:go ioutil.ReadFile" },
  "steps": [
    { "run": "comby -in-place 'ioutil.ReadFile(:[1])' 'os.ReadFile(:[1])' .go", "container": "comby/comby" }
  ],
  "changeset_template": {
    "title": "Replace ioutil.ReadFile with os.ReadFile",
    "body": "ioutil.ReadFile is deprecated as of Go 1.16. This replaces it with os.ReadFile.",
    "branch": "batch/ioutil-to-os",
    "commit": { "message": "Replace ioutil.ReadFile with os.ReadFile" }
  }
}
```
```jsonc
Result {
  "valid": true,
  "warnings": [],
  "spec_yaml": "name: ioutil-to-os\ndescription: Replace deprecated ioutil.ReadFile with os.ReadFile (Go 1.16+)\non:\n    - repositoriesMatchingQuery: repo:^github\\.com/sourcegraph/ lang:go ioutil.ReadFile\nsteps:\n    - run: comby -in-place 'ioutil.ReadFile(:[1])' 'os.ReadFile(:[1])' .go\n      container: comby/comby\nchangesetTemplate:\n    title: Replace ioutil.ReadFile with os.ReadFile\n    body: ioutil.ReadFile is deprecated as of Go 1.16. This replaces it with os.ReadFile.\n    branch: batch/ioutil-to-os\n    commit:\n        message: Replace ioutil.ReadFile with os.ReadFile\n"
}
```

Rendered, that `spec_yaml` is the canonical, human-reviewable artifact — note the
unquoted `on:` (the emitter forces it back from YAML's reserved-word quoting):

```yaml
name: ioutil-to-os
description: Replace deprecated ioutil.ReadFile with os.ReadFile (Go 1.16+)
on:
    - repositoriesMatchingQuery: repo:^github\.com/sourcegraph/ lang:go ioutil.ReadFile
steps:
    - run: comby -in-place 'ioutil.ReadFile(:[1])' 'os.ReadFile(:[1])' .go
      container: comby/comby
changesetTemplate:
    title: Replace ioutil.ReadFile with os.ReadFile
    body: ioutil.ReadFile is deprecated as of Go 1.16. This replaces it with os.ReadFile.
    branch: batch/ioutil-to-os
    commit:
        message: Replace ioutil.ReadFile with os.ReadFile
```

**4. Preview** (resolve what it *would* touch — no side effects).

> **Developer:** *What would this touch?*

```
Agent → bc_preview { "spec_yaml": "<the spec_yaml from step 3>" }
```
```jsonc
Result {
  "resolved_repos": ["github.com/sourcegraph/sourcegraph", "github.com/sourcegraph/src-cli", ...],
  "estimated_changesets": 7,
  "estimated_phases": 2,        // ceil(7 / 5) — planning only; staged rollout is governed and out of v1 scope
  "truncated": false,
  "validation": { "valid": true, "issues": [] },
  "boundary_note": "target resolution runs against the public API; step execution requires Enterprise executors and is out of scope"
}
```

**5. Request publish** (the thesis, demonstrated by refusal).

> **Developer:** *Ship it.*

```
Agent → bc_request_publish {
  "spec_yaml": "<the spec_yaml>",
  "approval": { "approver": "alan@example.com", "token": "<out-of-band token>" }
}
```
```jsonc
Result {
  "status": "NOT_IMPLEMENTED",
  "semantics_doc": "v1 composes, validates and previews — it never publishes. ...",
  "governance_semantics": {
    "default": "dry-run; publication requires explicit human approval — an invariant, not a feature flag",
    "v2_gate": "graduates only when the measurement layer exists (blast-radius scoring, CI-signal tiering, canary stop rule) — without risk-tiering, human approval of bespoke diffs at scale is theater",
    ...
  }
}
```

**Demonstrates:** the full composition arc, discovery output feeding the `on:`
clause, the dry-run boundary, and the governed refusal as the deliverable.

---

## Scenario 2 — Multiple queries, end to end

**Goal:** show `bc_find_targets` as the **`on:`-clause factory** — a batch spec's
`on:` is a *list* of rules, so the tool takes a list of queries, fans them out as
parallel searches, and returns their merged union (occurrence counts summed,
sample paths combined and re-capped, duplicate queries collapsed).

> **Developer:** *Find everywhere we use either of Go's two deprecated read
> helpers — `ioutil.ReadFile` or `ioutil.ReadAll`.*

```
Agent → bc_find_targets {
  "queries": [
    "repo:^github\\.com/sourcegraph/ lang:go ioutil.ReadFile",
    "repo:^github\\.com/sourcegraph/ lang:go ioutil.ReadAll"
  ],
  "max_repos": 50
}
```
```jsonc
Result {
  "targets": [
    // a repo matched by BOTH queries appears ONCE, counts summed, paths merged:
    { "repo": "github.com/sourcegraph/sourcegraph", "occurrence_count": 19, "sample_paths": ["...", "..."] },
    { "repo": "github.com/sourcegraph/src-cli", "occurrence_count": 4, "sample_paths": [...] }
  ],
  "normalized_queries": [
    "repo:^github\\.com/sourcegraph/ lang:go ioutil.ReadFile",
    "repo:^github\\.com/sourcegraph/ lang:go ioutil.ReadAll"
  ],
  "total_repos": 9,
  "truncated": false
}
```

From here the flow is identical to Scenario 1. The wrinkle worth narrating: v1
composition takes a **single** `on.repositoriesMatchingQuery`, so the agent
either picks the most relevant normalized query or — better for this case —
composes one query covering both helpers and re-previews:

```
Agent → bc_find_targets { "queries": ["repo:^github\\.com/sourcegraph/ lang:go ioutil.Read"] }
```

**Demonstrates:** parallel fan-out, union merge (dedup + summed counts), and the
honest v1 edge — discovery accepts many `on:` rules; composition narrows to one
(multi-`on` is rejected at parse time; see Scenario 4's invalid path).

---

## Scenario 3 — Missing information from the start

**Goal:** show the two validation gates catching an underspecified request
*before* anything reaches the network or a reviewer — and how the agent uses the
errors to drive the conversation rather than guessing.

**3a — A query that normalizes to nothing** (caught before any network call):

> **Developer:** *Open PRs everywhere.* *(no query, no transformation)*

If the agent tries to discover with an empty or whitespace query:

```
Agent → bc_find_targets { "queries": ["   "] }
```
```jsonc
Result (error) { "code": "INVALID_QUERY", "message": "invalid query" }
```

An empty list, or any query that normalizes to nothing, is `INVALID_QUERY` —
rejected locally, no upstream call. The agent's correct move is to ask the
developer *which* pattern and *which* repos.

**3b — A spec missing required fields** (the composition gate reports every
problem at once):

The developer names a pattern but never says what to change it *to*, what to call
the branch, or how to title the PR. The agent attempts composition with the gaps:

```
Agent → bc_create_spec {
  "name": "",
  "on": { "repositoriesMatchingQuery": "lang:go ioutil.ReadFile" },
  "steps": [],
  "changeset_template": { "title": "", "branch": "feature/bad..name", "commit": { "message": "" } }
}
```
```jsonc
Result (error) {
  "code": "VALIDATION_FAILED",
  "message": "invalid batch spec: name is required; at least one step is required; changesetTemplate.title is required; changesetTemplate.branch \"feature/bad..name\" is not a valid git branch name; changesetTemplate.commit.message is required"
}
```

Every field problem is listed in **one** response (so the developer fixes them in
one pass, not one per round-trip). The agent reads the list, asks the developer to
fill each gap, then recomposes — landing in Scenario 1, step 3.

**Demonstrates:** fail-fast local validation at both gates (`INVALID_QUERY` pre-network,
`VALIDATION_FAILED` with batched field detail), turning missing information into a
precise checklist instead of a malformed run.

---

## Scenario 4 — A spec is already in hand: preview from YAML

**Goal:** the tools are independent — you don't have to start at discovery. A
developer who already has a spec (handwritten, or from a previous session) goes
straight to `bc_preview`.

> **Developer:** *Here's a batch spec — what would it resolve to today?*
> *(pastes the YAML)*

```
Agent → bc_preview {
  "spec_yaml": "name: ioutil-to-os\non:\n    - repositoriesMatchingQuery: repo:^github\\.com/sourcegraph/ lang:go ioutil.ReadFile\nsteps:\n    - run: comby -in-place 'ioutil.ReadFile(:[1])' 'os.ReadFile(:[1])' .go\n      container: comby/comby\nchangesetTemplate:\n    title: Replace ioutil.ReadFile with os.ReadFile\n    body: Deprecated since Go 1.16.\n    branch: batch/ioutil-to-os\n    commit:\n        message: Replace ioutil.ReadFile with os.ReadFile\n"
}
```
```jsonc
Result {
  "resolved_repos": ["github.com/sourcegraph/sourcegraph", ...],
  "estimated_changesets": 7,
  "estimated_phases": 2,        // ceil(7 / 5) — planning only; staged rollout is governed and out of v1 scope
  "truncated": false,
  "validation": { "valid": true, "issues": [] },
  "boundary_note": "target resolution runs against the public API; step execution requires Enterprise executors and is out of scope"
}
```

**Invalid-spec path** (worth showing once): a spec with two `on:` rules is
rejected at parse, before any search:

```
Agent → bc_preview {
  "spec_yaml": "name: x\non:\n    - repositoriesMatchingQuery: lang:go a\n    - repositoriesMatchingQuery: lang:go b\nsteps:\n    - {run: 'true', container: alpine}\nchangesetTemplate: {title: x, body: x, branch: batch/x, commit: {message: x}}\n"
}
```
```jsonc
Result (error) { "code": "INVALID_SPEC", "message": "invalid spec" }
```

**Demonstrates:** `bc_preview` stands alone — no prior discovery or composition
required — and rejects a malformed spec locally with `INVALID_SPEC` before
spending an upstream call.

---

## Scenario 5 — Preview a batch change that already exists in the UI

**Goal:** parity / sanity check. Take the spec of a batch change that already
lives in the Sourcegraph UI and run it through this server — confirming the
write-port resolves the **same** targeting the UI would, and then refuses to
publish. This is the "import an existing campaign and see that it's governed"
story.

**1.** In the Sourcegraph UI, open the existing batch change and copy its spec
(the YAML in the batch change's *Spec* tab, or the local `*.batch.yaml` it was
applied from).

**2.** Hand it to the agent unchanged:

> **Developer:** *This batch change already exists in our instance. Preview it
> through the MCP server and confirm it resolves the same repos — but do not
> publish.*

```
Agent → bc_preview { "spec_yaml": "<the exact YAML from the UI>" }
```
```jsonc
Result {
  "resolved_repos": [ /* compare against the UI's resolved repository list */ ],
  "estimated_changesets": 42,
  "estimated_phases": 9,        // ceil(42 / 5) — planning only; staged rollout is governed and out of v1 scope
  "truncated": false,         // if true, the UI saw more; the search limit capped us — note it
  "validation": { "valid": true, "issues": [] },
  "boundary_note": "target resolution runs against the public API; step execution requires Enterprise executors and is out of scope"
}
```

**3.** Attempt to publish through the port — the governance statement:

```
Agent → bc_request_publish {
  "spec_yaml": "<the same YAML>",
  "approval": { "approver": "alan@example.com", "token": "<token>" },
  "rollout": { "mode": "staged", "initial_batch": 5, "halt_on_failure_rate": 0.2 }
}
```
```jsonc
Result { "status": "NOT_IMPLEMENTED", "governance_semantics": { ... } }
```

**Two honest notes for the narration:**
- The public instance won't have *your* enterprise batch change. To demo this
  literally, run the server against an instance that does (set `SG_BASE_URL` and
  `SG_ACCESS_TOKEN` to that instance). Against the public instance, reproduce the
  effect by previewing any real, well-known spec and comparing `resolved_repos`
  to the same query run in the public UI.
- A `truncated: true` here is the §2/§4 targeting-safety point made concrete: the
  UI may resolve more repos than the search limit returns, so `estimated_changesets`
  is a lower bound — surface it, don't paper over it.

**Demonstrates:** the port is interoperable with the existing artifact (same spec,
same targeting), the dry-run boundary holds even on a real campaign, and
publication is refused regardless of how legitimate the spec is — the invariant.

---

## Quick reference

**Error codes by tool** (client-facing `{code, message}`; the cause stays in the
server logs — see [`03-tool-contracts.md`](03-tool-contracts.md)):

| Tool | Codes |
|---|---|
| `bc_find_targets` | `INVALID_QUERY`, `UPSTREAM_UNAVAILABLE` |
| `bc_inspect_target` | `INVALID_INPUT`, `NOT_FOUND`, `TOO_LARGE`, `UPSTREAM_UNAVAILABLE` |
| `bc_create_spec` | `VALIDATION_FAILED` (field-level detail in the message) |
| `bc_preview` | `INVALID_SPEC`, `UPSTREAM_UNAVAILABLE` |
| `bc_request_publish` | — (always returns `NOT_IMPLEMENTED`, never errors) |

**Scenario → what it proves**

| # | Scenario | Proves |
|---|---|---|
| 1 | Single query, E2E | The full arc; discovery → `on:` clause → dry-run → governed refusal |
| 2 | Multiple queries, E2E | Parallel fan-out + union merge; the `on:`-clause factory |
| 3 | Missing info | Both validation gates fail fast with actionable detail |
| 4 | Preview from YAML | Tools are independent; local `INVALID_SPEC` rejection |
| 5 | Existing UI batch change | Targeting parity + the invariant holds on a real campaign |

---

## Recording checklist

For a short screen recording (the Cycle 4 "reproducible demo" deliverable):

1. Start the MCP client with the `bc` server connected; show the five tools in
   the tool list.
2. Run **Scenario 1** start to finish — it is the spine of the story.
3. Splice in **Scenario 3b** (the `VALIDATION_FAILED` batch) to show the
   guardrail, and **Scenario 5**'s `bc_request_publish` refusal to land the
   thesis.
4. Keep the server's **stderr** visible in a second pane: the structured
   `{request, error}` logs show the decoupled error model (code/message to the
   client, cause only in the log) without narration.
5. Keep it under ~3 minutes — discovery, one preview, one refusal. The dense
   argument lives in the docs; the recording exists to make the arc tangible.
