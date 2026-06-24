# Code-review recommendations (deferred)

Non-blocking findings from the per-slice code reviews in Cycle 3. Critical bugs
and architecture violations are fixed in-slice; everything here is a quality
improvement deliberately deferred, with enough context to action later.

Status legend: 🟡 worth doing · 🟢 nice-to-have

---

## Slice 1 — `batchspec` domain + `bc_create_spec`

**Review verdict:** no critical bugs, no architecture violations (depguard green;
`batchspec` imports nothing internal; `cmd/server` depends on `createspec`, not
`batchspec`).

- ✅ **FIXED — Canonical YAML quotes `"on":`.** `YAML()` now encodes to a
  `yaml.Node` and re-tags the `on` key as a plain `!!str` (`forcePlainKey`), so
  the emitter renders idiomatic unquoted `on:`. Locked by a test assertion.
  *File:* `internal/batchspec/yaml.go`.

- ✅ **FIXED — `dto` anonymous nested struct duplication.** Extracted named
  types (`onRule`, `stepDTO`, `changesetTemplateDTO`, `commitDTO`); `toDTO`/`Parse`
  now use compile-checked `stepDTO(…)`/`Step(…)` conversions. *File:*
  `internal/batchspec/yaml.go`.

- ✅ **FIXED — Multi-`on` rejection ordering.** `Parse` now checks
  `len(d.On) > 1` before composing. *File:* `internal/batchspec/yaml.go`.

- ✋ **DECIDED (won't change) — `Parse` lenient about unknown fields.** Kept
  lenient by decision: a misspelled *required* key leaves its field empty and is
  caught by `New` (or, for `body`, by the missing-body warning); unknown optional
  keys are harmless. Sharper `KnownFields` diagnostics deferred. Documented in
  `Parse`. *File:* `internal/batchspec/yaml.go`.

- ✋ **DECIDED (document, don't expand) — `branchRe` / suspicious-step are
  pragmatic subsets.** Full `git check-ref-format` (~40 rules) and step-determinism
  detection (undecidable in general) are disproportionate for a few-day POC;
  `branchRe` already blocks the dangerous chars and suspicious-step is only a
  warning. Limits documented in code. *File:* `internal/batchspec/spec.go`.

---

## Slice 2 — `preview` + `bc_preview`

**Review verdict:** no critical bugs, no architecture violations (depguard green;
`preview` owns its `Resolver` port and imports no transport; `sgResolver` adapter
sits at the composition root).

- 🟡 **`resolved_repos` / `estimated_changesets` are bounded by the search
  limit.** `sgResolver` calls `Search(…, 0)` (no client cap), but Sourcegraph's
  own result limit (and the query's `count:`) still caps what comes back, so a
  broad query under-reports what a real run would touch. The `truncated` flag is
  now surfaced (see holistic-review section ✅), so this is no longer *silent* —
  but for an accurate count, add `count:all` to the resolution query (or
  paginate). Ties to analysis doc §2. *File:* `cmd/server/main.go`
  (`sgResolver`), `preview/preview.go`.

- **✋ DECIDED (won't change) — `validation.valid` is always `true` in the output.** `batchspec.Parse`
  couples parse + validate, so an invalid spec errors as `INVALID_SPEC` before
  the validation block is built; the `valid:false` path is therefore
  unreachable. To make the field meaningful, expose a lenient parse in
  `batchspec` (parse without validating) and validate separately in `preview`,
  so a preview can resolve repos *and* report construction issues. (Same family
  as `bc_create_spec`'s always-true `valid`.) *Files:* `internal/batchspec`,
  `preview/preview.go`.

- ✅ **FIXED — `sgResolver` mislabeled a query-construction failure as upstream.**
  The `Resolver` port now takes a `targeting.Query` (consistent with
  `findtargets.Searcher`); `preview.Execute` constructs it and maps a
  construction failure to `INVALID_SPEC`, leaving the adapter to fail only on
  transport (`UPSTREAM_UNAVAILABLE`). *Files:* `preview/preview.go`,
  `cmd/server/main.go`.

---

## Slice 3 — `requestpublish` + `bc_request_publish`

**Review verdict:** no critical bugs, no architecture violations (contract stub;
imports nothing internal; never errors).

- 🟢 **`Execute` ignores its `Input` (`_ Input`).** Intentional for the v1
  contract stub — the `Input` type, the tool schema, and the handler's
  `BindArguments` exist to *document* the governed-publish interface; nothing
  consumes the bound values yet. The handler's full `Input` construction is
  therefore ceremonial in v1. Wire it when v2 implements publication (and record
  the approver as an audit event then). *Files:* `requestpublish/requestpublish.go`,
  `cmd/server/main.go`.

## Holistic Cycle-3 review (re-review of the full diff)

- ✅ **RESOLVED — `preview` discards the search truncation signal.** The
  `Resolver` port now returns a `Resolution{Repos, Truncated}`; `bc_preview`
  surfaces a top-level `truncated` bool (consistent with `bc_find_targets`) plus
  a validation issue, so a capped resolution is no longer silently reported as
  complete. Contract §4 updated. *Note:* `estimated_changesets` is still a lower
  bound when `truncated` (full enumeration via `count:all`/pagination remains a
  follow-up — see slice-2 item below). *Files:* `preview/preview.go`,
  `cmd/server/main.go` (`sgResolver`).

- 🟢 **`createspec.Execute` has an unreachable error branch.** `batchspec.New`
  only ever returns `*ValidationError` or `nil`, so the `errors.As` always
  matches and the `fmt.Errorf("compose spec: …")` fallback is dead. It's
  harmless defensive code; either drop it or add a comment that it guards a
  future change to `New`'s contract. *File:* `createspec/createspec.go`.

## Cross-cutting

- 🟢 **All five tools carry `destructiveHint: true`.** That's mcp-go's default
  for the unset field; it is ignored when `readOnlyHint` is true, but reads oddly
  for read-only tools. Add `mcp.WithDestructiveHintAnnotation(false)` to each
  tool definition for unambiguous annotations. *File:* `cmd/server/main.go`.
