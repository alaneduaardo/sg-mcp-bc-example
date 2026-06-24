# Code Plane, Post-Split: A Structured Read and One Phased Hypothesis

**Author:** Alan Eduardo · **Date:** June 2026 · **Status:** External analysis, hypothesis-grade

---

## Honesty header — scope of access

This analysis distinguishes between what I touched and what I read:

- **Hands-on:** public code search (sourcegraph.com/search), the `src` CLI against the public instance, the public GraphQL API, and a small Go MCP server I built on top of them (repo linked).
- **By documentation only:** the official MCP server and Batch Changes execution (both Enterprise-only). Claims about them cite primary sources — official docs, the public handbook, and the role description — and are framed as hypotheses where external visibility is limited.

Everything below applies a discipline I use professionally: claim → counter-argument → only then position. Where I propose something, I assume the team has likely already discussed it; the value offered is the execution reasoning, not the idea.

---

## §1 — Strategic read: what Code Plane is now

Since the December 2025 split — Amp spun out as an independent company; Sourcegraph refocused as enterprise code intelligence — the strategic position of Code Plane has sharpened, not blurred. Sourcegraph no longer competes in the inner loop. It competes as the **vendor-neutral infrastructure layer that agents from any vendor consume**: retrieval standardized through MCP, action at scale through Batch Changes.

Two published positions anchor this read:

1. Sourcegraph's own research on agent runs at scale identifies **infrastructure, not model capability, as the bottleneck** for agents operating on large codebases. [VERIFY: link the primary post/study]
2. The Code Plane charter (role description, 2026): products *for* developers **and for the agents acting on their behalf** — explicitly including an outer-loop agent that turns a prompt into a *staged, self-healing rollout with CI feedback loops and post-publish remediation*.

The org design supports the same read: teams are split by job-to-be-done — *understanding* code (search, navigation, Deep Search) versus *acting on* code (Batch Changes, Monitors, Insights, the integration surfaces: MCP server, `src` CLI). The name itself — data plane / control plane vocabulary — signals the separation between transport and decision.

Implication for everything that follows: the buyer is enterprise, the consumer is increasingly an agent, and the scarce asset is **governed action** — not another way to read code.

## §2 — Hands-on teardown (dual lens: human and agent)

*Method: I used public code search and the `src` CLI the way both a developer and a coding agent would — and wired both into a small Go MCP server to feel the integration surface directly.*

> [TO FILL during build — for each friction: what I did, what happened, why it matters for an agent consumer. Target 3–5 concrete, reproducible items. Candidate lenses: output parseability, error structure, token efficiency of responses, latency, auth ergonomics.]

- Friction 1: …
- Friction 2: …
- Friction 3: …

## §3 — Two organizational curiosities (verifiable from public sources)

**Curiosity 1 — Code Plane owns the pipe; the pipe carries the neighbor's product.**
The MCP server is owned by Code Plane, yet the tools it exposes today — search, file/repo browsing, commits, diffs, Deep Search — are all *read* capabilities belonging to the Code Understanding surface. None of Code Plane's own *action* surfaces (Batch Changes, Monitors, Insights) is reachable through it. The integration layer built by the action team currently transports only the understanding team's product. [VERIFY: current tool list of the default MCP endpoint vs. /all endpoint, from official docs]

**Curiosity 2 — The action arm and the measurement arm live in the same team and don't touch.**
Batch Changes executes mass change; Code Insights measures code over time. A batch change's post-merge repercussion — adoption, regression, reverts — is exactly what Insights exists to chart. No integration exists between them. [VERIFY: docs — absence of any Insights-over-Batch-Changes feature]

Neither curiosity is an accusation — sequencing is the most likely explanation for both. But they mark, precisely, where the leverage is.

## §4 — The hypothesis: a governed MCP write port over the Batch Changes domain

**In four sentences.** Code Plane owns the agent integration layer (MCP) and the action layer (Batch Changes), but they do not touch — MCP today only transports read capabilities from the neighboring team. I propose an MCP port over the Batch Changes domain — `bc_find_targets → bc_inspect_target → bc_create_spec → bc_preview → bc_request_publish` — **dry-run-only in v1, with human approval as an invariant**. The counter-hypothesis "the CLI is enough" fails for hosted agents (no configured shell or local credentials — the Stripe-Minions class of consumer), for enterprise governance (MCP's dedicated OAuth scope permits "propose but not publish"; shell access is all-or-nothing), and for uniform auditability. I would validate with 2–3 design partners already using MCP and Batch Changes simultaneously.

**The layer split that keeps this safe.** The proposal lives entirely in the *composition* layer: the agent assembles the existing declarative batch spec in conversation with the developer (search results literally become `on.repositoriesMatchingQuery`). The *execution* layer — how each diff is produced, deterministic container steps today, agent-generated steps on the declared roadmap — is untouched. Consequence:

| Spec composed by | Deterministic steps | Agent-generated steps (roadmap) |
|---|---|---|
| Human (today) | Classic risk; review amortizes | The determinism problem (see §5) |
| Agent via MCP (this proposal) | ≈ classic risk — spec remains a reviewable artifact | Maximum-risk combination; gated to v2 |

The structured YAML spec is not a limitation here — it is the guardrail: an agent that proposes a validatable, diffable, human-reviewable artifact *before* anything executes is the enterprise-viable shape of agent-driven change. Compare with the alternative (an agent with shell access pushing to 800 repos) and the spec turns from constraint into the safety feature.

**Assumed prior discussion.** MCP being read-only today is most plausibly deliberate sequencing, possibly a permission-model decision (a scope designed so that connecting an agent can never modify code). If so, the proposal becomes "how to evolve the permission model to support governed write" — a more interesting conversation, not a dead one.

## §5 — Why v1 is dry-run-only: the measurement gap (guardrails)

**The forward-looking core.** Classic Batch Changes economics rest on an unstated property: **determinism amortizes review.** A deterministic step produces the *same pattern* of change across 800 repos; a reviewer inspects 3 diffs, trusts the pattern, and review cost is O(1) in repo count. The declared roadmap — an outer-loop agent producing rollouts — breaks this: agent-generated changes are bespoke per repo, "reviewed 3, trust 797" stops being valid, and review cost returns to O(n), destroying the tool's reason to exist. **Determinism was the implicit safety mechanism; removing it requires replacing it with explicit measurement.** That measurement layer does not exist yet — which is exactly why this proposal's v1 refuses publication.

**Four layers, deliberately weighted left (predictive over reactive):**

1. **Blast radius before publication — using the asset only Sourcegraph owns.** The precise cross-repo code graph can answer cheaply: how many downstream references does this changeset touch? Is this repo a leaf or a hub? Score changesets by blast radius and order rollout by risk: leaves first, hubs last. [VERIFY: no public evidence this exists today; frame as "I found no evidence of", not "they lack"]
2. **Heterogeneous CI treated as signal, not only gate.** A green CI in a repo without tests is silence, not approval. Classify repos by test-signal strength (coverage, historical flakiness) and tier the human-review requirement accordingly — otherwise "human approval" of hundreds of bespoke diffs is theater.
3. **Canary with a statistical stopping rule** — the JD's "self-healing rollout" made operational: publish N, observe CI pass rate and post-merge signals, expand or freeze automatically; on freeze, the agent investigates (Deep Search) and proposes remediation, a human approves resumption. The existing rollout windows are the skeleton; the feedback loop is the missing piece.
4. **Reactive floor — named honestly as late resilience.** Post-merge measurement (Insights as backend, Monitors as regression detection, revert rate as ground truth, mass-revert as the final safety net) is the audit layer, **not** the resilience story. Two structural reasons it cannot be the strategy: (a) *MTTR asymmetry* — reverting merged code is not a traffic flip; it is another mass change subject to the same review/CI pipeline, orders of magnitude slower than a deployment rollback; (b) *attribution decay* — subtle semantic or performance regressions surface weeks later, under newer commits; measuring at +30 days tells you *that* something degraded, not cleanly *which batch* caused it.

**Two sharp edges named.**
- *Goodhart in the self-healing loop:* the moment a remediation agent optimizes for "CI green," CI stops being a measure and becomes a target — in weak-suite repos the loop converges to "tests pass," not "behavior correct," precisely where batch changes are most needed.
- *The circular substrate problem:* safety is a property of the client's substrate (tests, static analysis), not of the tool — so the tool is safest exactly where it is least needed. The vendor's honest escapes: measure and surface substrate quality instead of promising safety; synthesize signal where missing (agent-generated characterization tests before the real change); and the sequencing escape — **the first campaign on a new client is not the migration; it is the campaign that builds the substrate.** Bootstrap: use the tool to create its own safety conditions.

**Mechanism vs. policy.** Should Sourcegraph define reliability policies for clients? Hard-coding "CI green = healthy" inherits the substrate problem silently; client-only control makes impact unmeasurable. The resolved industry pattern: the vendor ships the **policy interface** (clients declare gates — "tier-1 repos: CI green + coverage delta ≥ 0 + perf check + named approver"; the rollout engine executes and audits them), plus opt-in default policy packs per stack. Precedents: admission controllers/OPA, required checks, policy-as-code. Auditability then exceeds the diff: the trail is *which invariants were verified before change*, not just *what changed*.

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
