# Architecture — MCP for Batch Changes (POC)

**Status:** Cycle 0, frozen for build. Changes require a written reason.

---

## Design axioms (declared, not implied)

This codebase mirrors the product's reality in its structure. Guiding set, in precedence order:

1. Clean Architecture (Screaming) · DDD · Hexagonal — structure expresses the domain, dependencies point inward.
2. Clean Code · SOLID.
3. DRY, YAGNI, KISS — **on collision with the above, these lose.** This is axiomatic to the model, accepted as a conscious trade against the flat Go default.

**The trade, in three lines (also the README paragraph):** the idiomatic flat layout assumes maintainer seniority and continuity — "split when it hurts" requires someone present who feels the pain and refactors. Consolidated architecture is insurance against team variance: it encodes long-term structure up front. I priced the non-default cost deliberately.

## Tree

```
/mcp/bc/
    cmd/server/        → main: wiring, flags, registers the 5 bc_* tools. Zero logic.
    findtargets/       → use case
    inspecttarget/     → use case
    createspec/        → use case
    preview/           → use case
    requestpublish/    → use case (contract + governance; not executable v1)
    internal/
        batchspec/     → the product's central artifact: aggregate, invariants, YAML
        targeting/     → Targets / normalized Query flowing from discovery to composition
        sgclient/      → Sourcegraph transport: HTTP, auth, GraphQL plumbing, error
                         mapping. Zero use-case logic.
```

Reading rule (no documentation needed): `cmd/` = entry, `internal/` = support — everything else at this level **is a use case**, by elimination. Progressive disclosure: the top level screams what the product does; navigate down for how.

## Import rules

- Use cases import `internal/*`. Never each other. **Lateral imports between use cases are forbidden — absolute.**
- `internal/batchspec` and `internal/targeting` import nothing internal. `internal/sgclient` imports nothing internal.
- `cmd/server` imports use cases; wires concrete deps explicitly (no DI framework).
- Enforcement: Go's `internal/` gives the outer boundary mechanically (compiler). The lateral rule is machine-checked via depguard:

```yaml
# .golangci.yml (excerpt)
linters-settings:
  depguard:
    rules:
      no-lateral-usecases:
        files: ["**/mcp/bc/findtargets/**", "**/mcp/bc/inspecttarget/**",
                "**/mcp/bc/createspec/**", "**/mcp/bc/preview/**",
                "**/mcp/bc/requestpublish/**"]
        deny:
          - pkg: "<module>/mcp/bc/findtargets"
          - pkg: "<module>/mcp/bc/inspecttarget"
          - pkg: "<module>/mcp/bc/createspec"
          - pkg: "<module>/mcp/bc/preview"
          - pkg: "<module>/mcp/bc/requestpublish"
        # each use case's own path is exempt for itself; finalize per-rule when wiring CI
```

## Per-use-case ownership

Each use case package owns its full vertical:

- Its **own narrow interface** over what it needs from code intelligence, declared in the consumer package (Go idiom + ISP). Example: `findtargets` declares `type Searcher interface { Search(ctx, Query) (targeting.Targets, error) }`; `sgclient` satisfies it.
- Its **own GraphQL query**, shaped to its need. Similar scaffolding across use cases is accepted: *"a little copying is better than a little dependency."* Shared **transport plumbing only** lives in `sgclient`.
- Its **own tests**: table-driven for logic; the use case tested against a fake of its own interface; `sgclient` tested against `httptest.Server` with canned GraphQL responses (the client is exercised for real, serialization included).

Density requirement: a package that ends up with 40 hollow lines indicts the structure. Each use case must contain real substance — query, interface, validation behavior, tests.

## Naming — two namespaces, two conventions

- **Protocol namespace (MCP tool names):** `bc_find_targets`, `bc_inspect_target`, `bc_create_spec`, `bc_preview`, `bc_request_publish`. The `bc_` prefix lives here: it disambiguates when a client connects this server alongside the official Sourcegraph MCP, and carries the product context where the user sees it.
- **Go package namespace:** lowercase, single word, no underscores (official style): `findtargets`, `createspec`… The path `bc/findtargets` carries the context; ubiquitous language is preserved in tool names and identifiers (`findtargets.Execute`, `targeting.Targets`, `batchspec.Spec`).

## Domain notes

- `batchspec.Spec` is the aggregate. Invariants guarded at the constructor (`batchspec.New(...) (Spec, error)`) — Go has no encapsulation beyond the package boundary, so validity is enforced at creation and on `Validate()`. The spec **is** YAML (the product's contract format), so (de)serialization lives with the aggregate — domain representation, not presentation.
- `targeting` holds the value objects that connect discovery to composition: `Query` (normalized, ready for `on.repositoriesMatchingQuery`) and `Targets`.
- One bounded context: **Batch Change Composition**. The five use cases are application services over the shared model — indivisible parts of the product, not sequential stages.
- Errors as values; wrap with context (`fmt.Errorf("…: %w", err)`); sentinel errors per use case where callers branch on them. `context.Context` first parameter, propagated everywhere.

## Prepared defenses (anticipated reviewer flags)

| Flag | Defense |
|---|---|
| "Interface with one implementation = YAGNI violation" | The interface exists for testability (use case tested without network), not for hypothetical second impls. Tests that use the fake are the proof — interface without a test using it is confessed ceremony. |
| "Five packages for five small tools" | Density answers it; plus the README trade paragraph: insurance against maintainer variance, priced consciously. |
| "Why not flat?" | "The flat default assumes maintainer continuity and seniority; consolidated architecture is insurance against team variance; I accepted the non-default tax for that property." |
| Duplicated query scaffolding | Go proverb: a little copying over a little dependency. Shared transport only. |
