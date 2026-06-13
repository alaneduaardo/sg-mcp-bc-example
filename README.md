# MCP for Batch Changes — a governed write port (POC)

> **Status:** POC v1 · **dry-run only** · human approval is an invariant, not a feature flag · 2 of 5 tools implemented.

An exploration of one idea: expose Sourcegraph **Batch Changes** through an **MCP** server so that an agent can *compose* a batch change in conversation with a developer — discover targets, inspect code, assemble and validate the declarative spec, preview what it would touch — while **publication stays behind explicit human approval**.

This repository is two things at once: a small, runnable artifact, and the reasoning behind it. The reasoning lives in [`docs/`](docs/) and is the primary deliverable; the code exists to make the argument concrete and to feel the integration surface first-hand.

---

## The proposal, in brief

Sourcegraph's Code Plane owns both the agent integration layer (MCP) and the action layer (Batch Changes) — but today MCP only transports *read* capabilities from the neighboring Code Understanding surface. This POC sketches an MCP **write port** over the Batch Changes domain:

```
bc_find_targets → bc_inspect_target → bc_create_spec → bc_preview → bc_request_publish
```

The structured batch spec is not a limitation — it is the guardrail: an agent that proposes a *validatable, diffable, human-reviewable* artifact before anything executes is the enterprise-viable shape of agent-driven change. v1 deliberately refuses to publish, because the measurement layer that would make publication safe (blast-radius scoring, CI-signal tiering, canary stop rules) does not yet exist.

**The full argument — strategic read, hands-on teardown, the hypothesis, and why v1 is dry-run-only — is in the dense documents. Start here:**

| Doc | What it covers |
|---|---|
| [`01-code-plane-analysis.md`](docs/01-code-plane-analysis.md) | The complete analysis: strategy, the two organizational ironies, the hypothesis, the measurement gap |
| [`02-architecture.md`](docs/02-architecture.md) | Design axioms, the package tree, import rules, prepared defenses |
| [`03-tool-contracts.md`](docs/03-tool-contracts.md) | Input/output/error contracts for all five tools + the demo narrative |
| [`04-build-plan.md`](docs/04-build-plan.md) | Build cycles and the primary-source verification checklist |

---

## The tools

| Tool | Purpose | Status |
|---|---|---|
| `bc_find_targets` | Turn a search query into batch-change targeting (the `on:` clause factory) — per-repo counts + sample paths + a normalized query | ✅ implemented |
| `bc_inspect_target` | Fetch full file content in the context of a target, to ground a transformation | ✅ implemented |
| `bc_create_spec` | Compose and validate the declarative batch spec (pure; never executes) | ⏳ planned |
| `bc_preview` | Resolve what the spec *would* touch, without touching anything | ⏳ planned |
| `bc_request_publish` | Contract only — returns `NOT_IMPLEMENTED` plus the governance semantics. The refusal *is* the deliverable | ⏳ planned (contract) |

**Public-API boundary:** target resolution and file reading run for real against the public Sourcegraph instance. Step execution and publication are Enterprise surfaces and are **out of scope** — `bc_request_publish` ships as a documented contract, not an implementation.

---

## Repository structure

```
mcp/bc/
    cmd/server/        → MCP entry point: wiring + tool registration (stdio)
    cmd/smoke/         → throwaway CLI: hit the public instance directly
    findtargets/       → use case (bc_find_targets)
    inspecttarget/     → use case (bc_inspect_target)
    createspec/        → use case (planned)
    preview/           → use case (planned)
    requestpublish/    → use case (planned, contract-only)
    internal/
        batchspec/     → the product's central artifact: aggregate, invariants, YAML (planned)
        targeting/     → value objects: normalized Query + resolved Targets
        sgclient/      → Sourcegraph transport: HTTP, auth, GraphQL, error mapping
docs/                  → the analysis and design documents (the primary deliverable)
```

**Reading rule (no extra docs needed):** `cmd/` is the entry point, `internal/` is support — everything else at that level **is a use case**, by elimination. The top level screams what the product does; navigate down for how. Use cases own their full vertical (their own narrow interface, query, and tests) and **never import each other** — a rule enforced mechanically (see [Static analysis](#static-analysis) below).

---

## The architecture trade

This layout is deliberately *not* the idiomatic flat Go default, and that cost was priced on purpose:

> The flat layout assumes maintainer seniority and continuity — "split when it hurts" requires someone present who feels the pain and refactors. Consolidated architecture is insurance against team variance: it encodes long-term structure up front.

On collision, Clean Architecture / DDD / Hexagonal win over DRY/YAGNI/KISS here — a conscious trade, documented rather than implied. See [`02-architecture.md`](docs/02-architecture.md) for the full axiom set and the prepared defenses.

---

## Getting started

**Prerequisites:** Go 1.25+.

```sh
# Run the MCP server over stdio (defaults to the public instance).
go run ./mcp/bc/cmd/server

# Ad-hoc transport check against real public code (no MCP client needed):
go run ./mcp/bc/cmd/smoke -q 'lang:go errgroup.WithContext count:20' -n 5
go run ./mcp/bc/cmd/smoke -file github.com/moov-io/watchman -path internal/download/download.go
```

**Connecting an MCP client** (e.g. Claude Code) — build a binary and point the client at it over stdio:

```sh
go build -o bin/bc-server ./mcp/bc/cmd/server
```

```json
{
  "mcpServers": {
    "bc": { "command": "/absolute/path/to/bin/bc-server" }
  }
}
```

**Authentication:** the public instance needs none. For an enterprise instance, set `SRC_ACCESS_TOKEN` (sent as `Authorization: token …`) and override the endpoint with `-endpoint https://<your-instance>/.api/graphql`.

---

## Development

```sh
go test ./...        # full suite (unit tests use httptest with canned GraphQL)
go test -race ./...  # race-enabled
go vet ./...
```

### Pre-commit hook (caveat: must be activated)

A tracked hook in [`.githooks/pre-commit`](.githooks/pre-commit) runs **gofmt → go vet → golangci-lint → go test** and aborts the commit on any failure. Because `.git/hooks/` is not versioned, the hook must be activated **once per clone**:

```sh
git config core.hooksPath .githooks
```

Bypass for a single commit (e.g. WIP) with `git commit --no-verify`.

### Static analysis

The hook runs `golangci-lint` if it is installed, and **skips it with a warning if not**. That tool carries the real architectural enforcement: the `depguard` rule in [`.golangci.yml`](.golangci.yml) forbids lateral imports between use cases. Without it, only `go vet` runs and that invariant is **not** checked. Install it for full coverage:

```sh
brew install golangci-lint   # or see https://golangci-lint.run
```

---

## Scope and caveats

- **Dry-run only.** v1 composes, validates and previews. It never executes or publishes. This is an invariant.
- **Hypothesis-grade.** The analysis is an external read; claims about Enterprise-only surfaces are framed as hypotheses, with a primary-source verification checklist in [`04-build-plan.md`](docs/04-build-plan.md).
- **Not a generic search tool.** `bc_find_targets` shapes output for spec composition, not browsing — the official Sourcegraph MCP already exposes generic read tools, and duplicating them would be incoherent.
