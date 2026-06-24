# Build Plan — cycles and palpable outcomes

Working mode: iterative cycles, each ending in something runnable or readable. Cycle 0 (design) is closed by the three companion documents. Dual intent throughout: **(A)** the analysis artifact, **(B)** Go unrust through real construction — the build is the training, so breadth of language surface is a feature, not waste.

---

## Cycle 1 — Sourcegraph client in Go (Sat AM)

Build `internal/sgclient` + `internal/targeting`, test-first.

- GraphQL search + file fetch against the public instance.
- Tests: `httptest.Server` with canned GraphQL responses (client exercised for real, serialization included); table-driven for query normalization.
- Unrust surface: structs/JSON marshalling, HTTP client, error wrapping (`errors.Is/As`), table-driven tests, package organization, `context.Context` propagation.

**Palpable:** `go test ./...` green + a throwaway `main` searching real public code.

## Cycle 2 — MCP port with the read tools (Sat PM) → **POC v1**

- `findtargets` and `inspecttarget` use cases (own interfaces, own queries, own tests with fakes).
- `cmd/server` registering `bc_find_targets`, `bc_inspect_target` via mcp-go.
- §2 of the analysis doc fills itself here: using public search + `src` CLI to test my own client *is* the hands-on; log every friction immediately into the doc.
- Unrust surface: interfaces (consumer-side), DI by explicit wiring, third-party SDK integration.

**Palpable (= v1, offerable):** Claude Code connected to the server, searching real code + analysis doc with §1–§4 written, §5–§6 drafted. If the HM intro lands Tuesday, this smaller-but-palpable version is what gets offered.

## Cycle 3 — Batch Spec domain (window: screen → HM intro)

- `internal/batchspec`: aggregate, constructor-guarded invariants, full YAML validation.
- `createspec` and `preview` use cases — functional to the public-API boundary (composition + local dry-run; execution declared out of scope, Enterprise).
- `requestpublish` as documented contract returning NOT_IMPLEMENTED + governance semantics.
- Unrust surface: YAML, domain modeling, validation, more tests.

**Palpable:** the full demo narrative — conversation → validated spec → preview → governed refusal.

## Cycle 4 — Polish (following gates; keeps Go warm until the technical interview)

- Concurrency where natural: parallel search queries in `findtargets` (goroutines/channels with real purpose).
- Adversarial pass on the doc: read as the internal skeptic — "we've discussed this; why does he think it's new?" Every claim framed as hypothesis with the external-visibility caveat.
- English pass; reproducible demo (script or short recording); README final paragraph (the priced trade).

---

## Verification checklist (primary sources — before the doc is ever shared)

1. MCP default endpoint tool list vs `/all` (official docs) → backs Curiosity 1.
2. Batch spec YAML reference: `on.repositoriesMatchingQuery`, steps shape → backs contracts and the targeting link.
3. Rollout windows mechanics (docs) → backs §5 layer 3.
4. MCP OAuth scope (docs) → backs governance claim.
5. Amp split announcement + "infrastructure is the bottleneck" post → backs §1.
6. Absence of Insights↔Batch Changes integration (docs) → backs Curiosity 2.
7. Current values page (handbook) — separate purpose: values interview prep, read the night before.

Standing rule: primary sources only. Search snippets and AI outputs are leads to confirm, never facts to cite. (The `codingAgent` incident is the precedent: a search-index artifact survived two exchanges until a direct fetch killed it.)

## Out of scope — listed so it stays out

- Any direct-publish implementation.
- Insights/Monitors integration (event-push mismatches MCP request/response; and prediction-platform territory is M4–M5 altitude — one line as future direction, no build).
- Generic read tools duplicating the official MCP surface.
