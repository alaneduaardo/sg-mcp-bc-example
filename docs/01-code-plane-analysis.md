# Code Plane, Post-Split: A Structured Read and One Phased Hypothesis

**Author:** Alan Eduardo · **Date:** June 2026 · **Status:** External analysis, hypothesis-grade

---

## Honesty header — scope of access

This analysis distinguishes between what I touched and what I read:

- **Hands-on:** public code search (sourcegraph.com/search), the `src` CLI against the public instance, the public GraphQL API, and a small Go MCP server I built on top of them (repo linked).
- **By documentation only:** the official MCP server and Batch Changes execution (both Enterprise-only). Claims about them cite primary sources — official docs, the public handbook, and the role description — and are framed as hypotheses where external visibility is limited.

**Everything here is built entirely from public information.** I had no internal access; the read of org structure, team boundaries, and product surface is *inferred* from public material, not reported from inside. The sources used:

- the **role description** for Code Plane (org design, charter, the outer-loop direction);
- the public **product documentation** (MCP server, Batch Changes, Code Insights, rollout windows, auth);
- the **changelogs** and the **Enterprise feature/pricing pages** (what ships where, and to whom);
- public **code search**, the **`src` CLI**, and the **GraphQL API** of sourcegraph.com (the hands-on surface of §2);
- the public **handbook** and the December 2025 split announcement.

Where the analysis states an organizational fact ("owned by", "in the same team"), read it as *the most consistent reading of these public sources*, not as an insider's claim. Everything below applies a discipline I use professionally: claim → counter-argument → only then position. Where I propose something, I assume the team has likely already discussed it; the value offered is the execution reasoning, not the idea.

---

## §1 — Strategic read: what Code Plane is now

Since the December 2025 split — Amp spun out as an independent company; Sourcegraph refocused as enterprise code intelligence — the strategic position of Code Plane has sharpened. With Amp gone, Sourcegraph's own product surface no longer fields an inner-loop coding assistant; it positions instead as the **vendor-neutral infrastructure layer that agents from any vendor consume**: retrieval standardized through MCP, action at scale through Batch Changes.

Two published positions anchor this read:

1. Sourcegraph's own research on agent runs at scale identifies **infrastructure, not model capability, as the bottleneck** for agents operating on large codebases. [VERIFY: link the primary post/study]
2. The Code Plane charter (role description, 2026): products *for* developers **and for the agents acting on their behalf** — explicitly including an outer-loop agent that turns a prompt into a *staged, self-healing rollout with CI feedback loops and post-publish remediation*.

The org design supports the same read: teams are split by job-to-be-done — *understanding* code (search, navigation, Deep Search) versus *acting on* code (Batch Changes, Monitors, Insights, the integration surfaces: MCP server, `src` CLI). The "Code Plane" naming even echoes data-plane / control-plane vocabulary — a suggestive signal I hold lightly, not a load-bearing claim.

Implication for everything that follows: the buyer is enterprise, the consumer is increasingly an agent, and the scarce asset is **governed action** — not another way to read code.

## §2 — Hands-on teardown (dual lens: human and agent)

*Method: I used public code search and the `src` CLI the way both a developer and a coding agent would — and wired both into a small Go MCP server to feel the integration surface directly.*

Five frictions, each reproducible against the public instance and each carried into a design decision in the server:

**Friction 1 — Search results are a union type; a file list isn't a file list.** `search.results.results` returns a heterogeneous array — `FileMatch`, `Repository`, `CommitSearchResult` — discriminated only by `__typename`. *What happened:* a consumer that treats the array as files folds repo- and commit-shaped entries into its counts without complaint. *Why it matters for an agent:* the shape forces type-discrimination before any aggregation; skip it and the proposed target cohort is built from mixed result kinds. The client filters to `FileMatch` explicitly before folding per-repo.

**Friction 2 — Errors arrive as HTTP 200.** A rejected query (unknown filter, bad field) returns `200 OK` with a populated `errors` array and `data: null`; only transport failures use a non-2xx status. *What happened:* keying on the status code reported success, then decoding produced an empty set indistinguishable from "nothing matched." *Why it matters for an agent:* the failure signal is in-band, not in the status line — the consumer must separate "the instance is unreachable" from "the query was rejected" itself, or it treats a malformed query as an empty cohort and marches on. The client maps the two to distinct errors (`ErrUpstreamUnavailable` vs. `GraphQLError`).

**Friction 3 — Truncation is silent unless you read `limitHit`.** Search returns `matchCount` and `limitHit`, but a result set capped by the instance is shaped identically to a complete one. *What happened:* without surfacing `limitHit`, "47 repos" and "47 repos, capped" are the same value. *Why it matters for an agent:* this is the targeting-safety hinge of §4 — an undetected cap means a campaign composed over a partial cohort. `bc_find_targets` propagates a `Truncated` flag for exactly this reason.

**Friction 4 — "Not found" is nulls along the chain, not an error.** Fetching a missing repo/revision/file returns `200` with no `errors` and `null` at the failing level of `repository → commit → file`. *What happened:* there is no 404 and no message; absence is encoded structurally. *Why it matters for an agent:* "does this file exist?" requires defensive null-checking at every level, and the consumer must synthesize the not-found signal — a clean error contract to branch on does not come for free.

**Friction 5 — Content is a second round-trip; cohort inspection is N+1.** Search returns repository and path only; full content requires a separate query per file. *What happened:* inspecting a 40-repo cohort is 40 additional fetches, each its own request and its own tokens. *Why it matters for an agent:* read cost scales with cohort size, so the tool boundary matters — discovery (cheap: counts + a few sample paths) is split from inspection (expensive: full content, opt-in per target) so an agent never pays for content it doesn't read.

## §3 — Two organizational curiosities (verifiable from public sources)

**Curiosity 1 — Code Plane owns the pipe; the pipe carries the neighbor's product.**
The MCP server sits within Code Plane's scope, yet the tools it documents today — search, file/repo browsing, commits, diffs, Deep Search — are all *read* capabilities of the Code Understanding surface. Per the public tool list, none of Code Plane's own *action* surfaces (Batch Changes, Monitors, Insights) is exposed through it: the integration layer carries only the understanding surface's product. [VERIFY: default MCP endpoint tool list vs. /all endpoint, from official docs]

**Curiosity 2 — The action arm and the measurement arm live in the same team and don't touch.**
Batch Changes executes mass change; Code Insights measures code over time. A batch change's post-merge repercussion — adoption, regression, reverts — is exactly what Insights exists to chart. Yet the public docs describe no integration between them — no Insights view defined over a batch change's outcomes. [VERIFY: docs — absence of any Insights-over-Batch-Changes feature]

Neither curiosity is an accusation — sequencing is the most likely explanation for both. But they mark, precisely, where the leverage is.

## §4 — The hypothesis: a governed MCP write port over the Batch Changes domain

**In four sentences.** Code Plane owns the agent integration layer (MCP) and the action layer (Batch Changes), but they do not touch — MCP today only transports read capabilities from the neighboring team. I propose an MCP port over the Batch Changes domain — `bc_find_targets → bc_inspect_target → bc_create_spec → bc_preview → bc_request_publish` — **dry-run-only in v1, with human approval as an invariant**. The counter-hypothesis "the CLI is enough" fails for hosted agents (no configured shell or local credentials — the Stripe-Minions class of consumer), for enterprise governance (MCP's auth model can express a scope that permits "propose but not publish" — shell access is all-or-nothing) [VERIFY #6: confirm the granularity of the MCP OAuth scope; the argument holds on "can express", not on a scope shipping today], and for uniform auditability. I would validate with 2–3 design partners already using MCP and Batch Changes simultaneously.

**The layer split that keeps this safe.** The proposal lives entirely in the *composition* layer: the agent assembles the existing declarative batch spec in conversation with the developer (search results literally become `on.repositoriesMatchingQuery`). The *execution* layer — how each diff is produced, deterministic container steps today, agent-generated steps on the declared roadmap — is untouched. Consequence:

| Spec composed by | Deterministic steps | Agent-generated steps (roadmap) |
|---|---|---|
| Human (today) | Classic risk; review amortizes | The determinism problem (see §5) |
| Agent via MCP (this proposal) | ≈ classic risk — spec remains a reviewable artifact | Maximum-risk combination; gated to v2 |

The structured YAML spec is not a limitation here — it is the guardrail: an agent that proposes a validatable, diffable, human-reviewable artifact *before* anything executes is the enterprise-viable shape of agent-driven change. Compare with the alternative (an agent with shell access pushing to 800 repos) and the spec turns from constraint into the safety feature.

**Assumed prior discussion.** MCP being read-only today is most plausibly deliberate sequencing, possibly a permission-model decision (a scope designed so that connecting an agent can never modify code). If so, the proposal becomes "how to evolve the permission model to support governed write" — a more interesting conversation, not a dead one.

## §5 — Why v1 is dry-run-only: the measurement gap (guardrails)

**The forward-looking core.** Classic Batch Changes economics rest on an unstated property: **determinism amortizes review.** A deterministic step produces the *same pattern* of change across 800 repos; even a team that opens every diff is checking one pattern repeated, so the *marginal* review cost of each additional repo trends toward zero — review is sublinear in repo count. The declared roadmap — an outer-loop agent producing rollouts — breaks this: agent-generated changes are bespoke per repo, each diff is a fresh judgment, and review cost returns to O(n), destroying the tool's reason to exist. **Determinism was the implicit safety mechanism; removing it requires replacing it with explicit measurement.** That measurement layer does not exist yet — which is exactly why this proposal's v1 refuses publication.

**Four layers, deliberately weighted left (predictive over reactive):**

1. **Blast radius before publication — using the asset only Sourcegraph owns.** The cross-repo code graph is the right basis to *approximate* a question no shell-based agent can ask: how many downstream references does this changeset touch — is this repo a leaf or a hub? I want to be precise about the limit: within-repo and indexed cross-repo references are tractable today; full transitive, cross-language blast radius is not a solved, cheap lookup — it is exactly the hard part. The proposal is to use the graph to *score and order* rollout by approximate blast radius (leaves first, hubs last), not to claim the graph already computes exact impact. [VERIFY: I found no public evidence a blast-radius score over Batch Changes exists today — frame as "found no evidence of", not "they lack"]
2. **Heterogeneous CI treated as signal, not only gate.** A green CI in a repo without tests is silence, not approval. Classify repos by test-signal strength (coverage, historical flakiness) and tier the human-review requirement accordingly — otherwise "human approval" of hundreds of bespoke diffs is theater.
3. **Canary with a statistical stopping rule** — the JD's "self-healing rollout" made operational: publish N, observe CI pass rate and post-merge signals, expand or freeze automatically; on freeze, the agent investigates (Deep Search) and proposes remediation, a human approves resumption. The existing rollout windows are the skeleton; the feedback loop is the missing piece.
4. **Reactive floor — named honestly as late resilience.** Post-merge measurement (Insights as backend, Monitors as regression detection, revert rate as ground truth, mass-revert as the final safety net) is the audit layer, **not** the resilience story. Two structural reasons it cannot be the strategy: (a) *MTTR asymmetry* — reverting merged code is not a traffic flip; it is another mass change subject to the same review/CI pipeline, orders of magnitude slower than a deployment rollback; (b) *attribution decay* — subtle semantic or performance regressions surface weeks later, under newer commits; measuring at +30 days tells you *that* something degraded, not cleanly *which batch* caused it.

**Two sharp edges named.**
- *Goodhart in the self-healing loop:* the moment a remediation agent optimizes for "CI green," CI stops being a measure and becomes a target — in weak-suite repos the loop converges to "tests pass," not "behavior correct," precisely where batch changes are most needed.
- *The circular-foundation problem:* safety is a property of the client's own foundation (tests, static analysis), not of the tool — so the tool is safest exactly where it is least needed. The vendor's honest escapes: measure and surface that foundation's quality instead of promising safety; synthesize signal where it's missing (agent-generated characterization tests before the real change); and the sequencing escape — **the first campaign on a new client is not the migration; it is the campaign that builds the foundation.** This last one is a go-to-market tension, not only a clever bootstrap: a buyer hears "your first campaign writes our tests, not our migration" as delayed time-to-value. The honest move is to price and position that first campaign as the foundation-building it is — surfacing the commercial edge, not hiding it behind the technical one.

**Mechanism vs. policy.** Should Sourcegraph define reliability policies for clients? Hard-coding "CI green = healthy" inherits the circular-foundation problem silently; client-only control makes impact unmeasurable. The resolved industry pattern: the vendor ships the **policy interface** (clients declare gates — "tier-1 repos: CI green + coverage delta ≥ 0 + perf check + named approver"; the rollout engine executes and audits them), plus opt-in default policy packs per stack. Precedents: admission controllers/OPA, required checks, policy-as-code. Auditability then exceeds the diff: the trail is *which invariants were verified before change*, not just *what changed*.

## §6 — Validation, costs, and what I'd cut

- **Validation:** 2–3 design partners with simultaneous MCP + Batch Changes usage (the Stripe profile). First milestone: agents *proposing* specs that humans publish, measured by proposal-acceptance rate — before any publish capability ships.
- **COGS in one line:** agent-mediated operations consume AI credits; any write surface needs a metering answer on day one.
- **Cut first:** any direct-publish path in v1; policy packs beyond one reference stack; Monitors-triggered automation (event-push doesn't fit MCP's request/response shape — a webhooks/automation surface, deliberately out of scope here).
- **Explicitly out of scope (altitude):** Insights as a tech-evolution prediction platform. Real opportunity, wrong altitude for an M3 team charter — noted in one line as future direction, by design.

---

## Source ledger (verify before sharing)

| # | Claim | Primary source to confirm |
|---|---|---|
| 1 | Amp spin-out / refocus, Dec 2025 | Official announcement |
| 2 | "Infrastructure is the bottleneck" study | Sourcegraph blog/research post |
| 3 | MCP default endpoint tool list (read-only; curated vs /all) | docs.sourcegraph.com MCP pages |
| 4 | `on.repositoriesMatchingQuery` + steps shape | Batch spec YAML reference |
| 5 | Rollout windows mechanics | Batch Changes docs |
| 6 | MCP dedicated OAuth scope | MCP/auth docs |
| 7 | No Insights↔Batch Changes integration | Docs (absence) |
| 8 | Outer-loop agent direction | Role description (verbatim) |

Rule applied throughout: primary sources only; search snippets and AI outputs are leads, never facts.
