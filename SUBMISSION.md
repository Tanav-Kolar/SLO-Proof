# SLOProof — evidence for recovery decisions

18 September 2026.

A candidate that improves on a broken version is not necessarily a fixed version.
SLOProof makes that distinction enforceable: freeze the acceptance criteria before
you touch routing, run a bounded trial, and let a read-only verifier decide against
the frozen contract. The verdict comes back as PROVED, FAILED or INCONCLUSIVE with
every query, parameter and window boundary attached, so anyone can re-run it.

We did re-run it. Verifying the three retained trials again, hours after the fact,
against live Elasticsearch, reproduced all three verdicts byte-identical apart from
the timestamp. That reproducibility is the product.

## Team

- Team name: tyr
- Project name: SLOProof
- Members: Vaibhav, Tanav, Shreyansh

## Problem

An incident is open. Someone proposes a fix, a slice of traffic is moved onto it, and
a decision now has to be made under pressure by whoever is most tired: **did that
actually work?**

Three answers are usually within reach. None of them settle it.

**"Errors went down."** Down from what? During an incident, the thing you are
comparing against is broken. A candidate that halves a 20% error rate looks like a
triumph and still misses a 99% success target by a mile. Scoring a candidate against
the version it replaces is the wrong frame at exactly the moment you depend on it,
because the baseline *is* the incident.

**"The dashboard looks healthy."** An empty result and a healthy result render
identically. If the candidate's traffic never reached its pods, or its records never
reached the index, the graph is flat — and the flatness means nothing at all. Absence
of evidence is not evidence of recovery, and most tooling cannot tell you which one
it is showing you.

**"The assistant says it's fixed."** A model can read the telemetry and produce a
fluent, well-cited paragraph concluding that recovery happened. It can produce the
same paragraph when recovery did not happen. Nothing in that output is a criterion
anyone can check afterwards, and nothing stops the next run from reaching a different
conclusion on identical data.

The difficulty is not measurement. It is four properties that have to hold at once:

- **The criteria must be fixed before anyone sees the result.** Otherwise the
  threshold drifts to meet the number, and nobody catches it, because the person
  adjusting it is the person who wants to ship.
- **The measurement must provably cover the cohort you routed to.** A verdict about
  10% of traffic is worthless unless you can show that the 10% is what was measured.
- **Missing data must be its own outcome** — not a quiet pass, and not a failure
  either, since those demand different responses.
- **Whoever benefits from a pass must not be able to grant one.** If the component
  that recommends the fix can also declare it fixed, the gate is decorative.

Miss any one and you get the failure we built this to prevent: shipping a candidate
that is better than the incident version and still broken, because relative
improvement read as success and the absolute target quietly stopped mattering.

The teams with this problem are platform and SRE groups already doing progressive
delivery. They have the routing controls and they have the telemetry. What they lack
is an answer to "is it fixed?" that survives the postmortem.

## How SLOProof answers it

Each of those four properties maps to a mechanism, not a promise.

**Fixed criteria.** An operator freezes a contract before any routing change. The
contract is data, not code — thresholds, window shape, eligibility rules and
freshness allowances in one JSON file, hashed into every artifact the run produces.

The thresholds are absolute: at least 400 candidate requests in each of three
non-overlapping 60-second windows, 99% success, p95 strictly below 300 ms. Nothing
in the contract refers to the version being replaced, so a broken baseline cannot
launder a broken candidate.

**Provable cohort coverage.** A bounded trial sends 10% of traffic to the candidate,
and the executor writes a receipt recording the weights it actually applied. An
in-cluster load generator records what the *client* observed, not what the server
reported. Each window then checks its observed candidate share against that receipt,
reconciles generated records against searchable ones, and confirms the candidate's
configuration never changed mid-window.

**Missing data is its own verdict.** INCONCLUSIVE is a first-class outcome, distinct
from FAILED. An empty result is never read as zero errors, and a run whose evidence
is incomplete blocks progression without implying the candidate is bad.

**The beneficiary cannot grant the pass.** The verifier is a separate Go binary
holding a credential that can read the evidence indices and write nothing —
enforced by the API key, verified live, and refused with HTTP 403 when it tries.
Only after the verdict is recorded does a deterministic policy turn it into a next
step, and only after that is a model asked for an opinion.

PROVED means the observed trial met its contract. Nothing more. It authorises one
guarded adjacent stage of an operator-started plan, which must freeze and measure
again. A verdict never moves traffic by itself, and FAILED never orders a rollback:
the version you came from may be worse than the one you are testing.

## The result that matters

In `demo-slow-20260918-02` the candidate beat the incident version on both contract
dimensions, and the gate refused it anyway.

| | Candidate (v1) | Incident version (v2) |
|---|---|---|
| Success rate | 100% | 80.00% |
| p95 latency | 507.6 ms | 857.6 ms |
| Verdict | **FAILED** — p95 must be strictly below 300 ms | — |

Any system that scores a candidate against the thing it replaces calls this a
recovery. The frozen contract is absolute, so it returned FAILED, left routing
unchanged and stopped the plan. The policy then re-scored the incident version
through the same window checks and reported `BOTH_VERSIONS_BREACH` — labelled as a
hypothesis about a shared cause, not a diagnosis, and explicitly not an instruction
to send traffic back to something worse.

## Evidence

Every number below comes from a retained artifact under `evidence/`, each carrying
the full ES|QL text, bound parameters, returned rows and exact window boundaries.

**Three outcomes under one contract** (hash `dd9fdf5a…`):

| Run | Candidate requests / window | Success | p95 per window | Verdict |
|---|---|---|---|---|
| `demo-healthy-20260918-01` | 600 / 600 / 600 | 100% | 57.23 / 57.28 / 57.34 ms | PROVED |
| `demo-slow-20260918-02` | 600 / 601 / 599 | 100% | 507.54 / 507.69 / 507.62 ms | FAILED |
| `demo-slow-20260918-01` | — | — | — | INCONCLUSIVE |

The healthy run reconciled 29,993 arrivals through to 29,993 searchable records,
zero dropped, zero indexing failures; the failed run, 29,992. The inconclusive run
had no observed-route receipt after its conditional execution was refused. Missing
evidence stayed missing — it did not become zero errors.

**A live staged rollout.** `rollout-20260918-s10` ran on EKS at 10:39 UTC today:
599 / 598 / 607 candidate requests, 100% success, p95 57.21 / 57.23 / 57.45 ms,
29,999 records reconciled end to end. PROVED, policy `CONTINUE_PLAN`, next weight
25%. The observed traffic split measured 0.0998 against a receipt claiming 0.10.

**The traffic split is checked, not assumed.** A route that drifts to full candidate
traffic is no longer the bounded slice the contract froze, so scoring it as one would
be wrong in both directions — a fast candidate would earn a PROVED it didn't run, and
a slow one a FAILED for a stage that never happened. Each window compares its observed
candidate share against the executor receipt within ±0.05 and returns INCONCLUSIVE on
divergence. This caught a real case: in `live-bad-recovery-20260918-01` the v2 fleet
collapsed mid-trial and two windows measured 100% candidate traffic while the receipt
said 10%.

**The separation is enforced by credentials, not convention.** Eleven live HTTP
authorization checks, re-run today, all pass — including five genuine 403s: the
verifier cannot write requests, contracts or the ledger; the ingest key cannot write
the ledger; the controller cannot write requests. An unrestricted credential presented
as the verifier key is refused and recorded.

**AI text is retained with its errors intact.** The three Bedrock agents explain both
measured trials with query citations. Early outputs we kept include a truncated
narrative, a count-denominator wording error and an overstatement of integrity-check
scope. They are advisory and the saved artifacts say so.

## Measured impact

Both versions were measured concurrently — same 60-second windows, same generator,
same client-observed records. A side-by-side observation, not a before/after, so it
isn't confounded by time, load or infrastructure drift.

Across the three windows of `demo-healthy-20260918-01` (180 seconds):

| | Candidate (v1), 10% of traffic | Incident version (v2), 90% |
|---|---|---|
| Requests | 1,800 | 16,196 |
| Successes | 1,800 | 12,956 |
| **Failed requests** | **0** | **3,240** |
| Success rate | 100% | 80.00% |
| p95 latency | 57.2–57.3 ms | 857.6 ms |

At the measured rate the incident version was failing roughly 1,080 checkout requests
per minute while, in the same windows, the candidate served its share with zero
failures at about one fifteenth the p95.

Contract freeze to recorded verdict, from artifact timestamps rather than estimates:

| Run | Freeze | Route effective | Verdict | Elapsed |
|---|---|---|---|---|
| PROVED | 08:22:55 | 08:23:14 (+20 s) | 08:28:08 | **313 s** |
| FAILED | 08:16:40 | 08:16:55 (+15 s) | 08:21:50 | **310 s** |

That elapsed time is the contract's own three windows plus its ingestion allowance
and the generator's run length. It is not bounded by human triage, and the decision
arrives with its evidence already attached.

**What we do not claim.** The request rate, the fault profiles and the 90/10 split
are ours, generated for a demo, so 1,080 failures per minute describes *this* injected
incident and no real service. We have no revenue-per-checkout figure and make no
revenue claim. We have no human-triage baseline and claim no MTTR reduction. A 10%
trial says nothing about behaviour at full production load. What we do claim is the
concurrent differential above, contract compliance, and that every number behind both
can be re-derived from the retained queries.

## Why a deterministic verifier

An agent may argue for a recovery. It cannot award itself a pass.

The contract hashes the exact query *text*, not just the thresholds, so a decision is
pinned to bytes that cannot drift between freeze and verdict. That is what makes
re-running it hours later reproduce the same answer, and it is the property a fluent
paragraph cannot offer no matter how well cited it is.

Everything a model produces sits downstream of that. The native workflow reads the
recorded verdict, requires exactly one, branches on the matching deterministic policy,
sends its Slack alert, and only then calls an agent. A run with no recorded decision
blocks the review before any agent call — we kept that execution as evidence:
`REVIEW_BLOCKED … 0 agent calls`.

Server-side OpenTelemetry spans are available and deliberately not used for the
verdict. In one window of `live-bad-recovery-20260918-01` the client observed 5,817
failed requests that produced no spans at all, because they were rejected before
reaching an instrumented pod — while the instrumented cohorts matched the client
counts exactly. Telemetry is blindest where an SLO most needs to see. Spans are
advisory context: `elastic/queries/otel-*.esql` and the `sloproof-otel-spans`
dashboard place them beside the contract evidence for comparison, and the verifier's
scoped credential is refused on that data with HTTP 403.

## Elastic integration

Elasticsearch Serverless stores client-observed request evidence, frozen contracts
and the decision ledger in separate indices under separate API keys. ES|QL is the
query language for every contract check.

Agent Builder provides an Investigator, a Verdict Explainer and an Evidence Briefer,
each restricted to three read-only tools with bounded queries. A native Elastic
Workflow, `sloproof-loop`, sequences the review and sends a Slack alert on every
deterministic outcome branch — passing stage, failed recovery and insufficient
evidence. All three were delivered live to `#all-on-call-engineering-team`:

| Outcome | Step | Receipt |
|---|---|---|
| PROVED | `notify_continue_rollout` | `ok: true`, ts `1789728283.725569` |
| FAILED | `notify_failed_rollout` | `ok: true`, ts `1789726542.717359` |
| INCONCLUSIVE | `notify_inconclusive_rollout` | `ok: true`, ts `1789726104.948059` |

The alerts carry the per-window numbers, the policy explanation and the contract
hash. No model output reaches them — a workflow test enforces that. The passing
alert states in its own text that it is not an authorization, because a green
Slack message is exactly the thing someone will read as approval.

The workflow has no routing step and no verdict-writing step. Workflow completion
is not an SLO pass.

## Demo

```bash
python3 scripts/render-evidence.py
python3 -m http.server 8768 --bind 127.0.0.1 --directory demo
```

`http://127.0.0.1:8768/` opens the decision challenge: predict each verdict, then see
the recorded one. It opens by asking whether you would ship a candidate with 100%
successful checkouts; the answer is no, for the reason in "The result that matters"
above. Offline export, no credentials.

Running it live, the presenter script and the stage-by-stage process view are all in
[the operator runbook](docs/DEMO_RUNBOOK.md),
[the showcase script](docs/SHOWCASE_SCRIPT.md) and
[`demo/process.html`](demo/process.html).

## Applying it to another service

The acceptance criteria live in [`contracts/checkout-slo.json`](contracts/checkout-slo.json):
window count and length, minimum samples, thresholds, eligible traffic classes,
freshness allowances. The verifier refuses to reinterpret a contract it does not
recognise, so changing a threshold is a reviewable diff rather than a code change.

[The operator setup guide](docs/configure-a-service.html) covers what a load generator
must emit, the index mappings, the scoped credential set, the route shape the executor
expects, and the queries the verifier runs. The initial customer is a platform or SRE
team already on Kubernetes with Gateway API and Elastic.

Reusable as-is: the contract schema, the ES|QL evidence queries, the credential
separation model, the ledger shape, the rollout policy. Service-specific: the checkout
fault profiles, the two-backend HTTPRoute shape, and the convention that v1 is the
candidate.

## Stack

- **Elastic** — Elasticsearch Serverless, ES|QL, separate request/contract/ledger
  indices, API-key privilege checks, Agent Builder, Workflows, Slack connector.
- **AWS** — EKS, ECR, Bedrock Converse for the standalone agent path.
- **Other** — Go, Python, Kubernetes Gateway API, Envoy Gateway, OpenTelemetry,
  static HTML.

Agent Builder uses Elastic's configured model connector, which is a different path
from the direct Bedrock agents; the Kibana Bedrock connector could not accept the
workshop credentials' session token. Sarvam and SageMaker are not used.

## Validation

- `go vet ./...` clean and `go test -race ./...` passing in both `src/control` and
  `src/checkout`, covering window evaluation, the observed-share check, rollout
  progression guards and retry classification.
- 22 boundary tests on the Elastic integration: unbounded queries, ungated agents,
  missing decision gates, notification branches that can be dropped, AI prose
  supplying an operational alert, and run-ID injection are each rejected.
- Four regression tests for the bounded evidence-retry runner.
- 11 live Elasticsearch authorization checks retained in `evidence/key-scopes.json`,
  plus a recorded refusal of an unrestricted credential presented as the verifier key.
- All three retained verdicts re-verified against live Elasticsearch and reproduced
  byte-identical apart from `verified_at`.

## Known limits

- Generated traffic and injected faults throughout. This is not an integration with a
  production release pipeline.
- The 25 / 50 / 100% stages are not live-validated. Stage 1 passed on EKS today; stage 2
  was refused because a concurrent load generator was active on the shared cluster —
  the concurrency guard doing its job, but the later stages remain unmeasured.
- Hashing gives traceability, not immutability. The controller holds ledger-write
  access and could forge records. This is not cryptographic attestation.
- No durable restart recovery, no independent expiry watchdog, no automatic reversal,
  no autonomous incident initiation.
- The retained INCONCLUSIVE cases are a missing receipt and a mid-trial route collapse,
  not a demonstrated telemetry-loss or successful-retry case.
- Agent Builder and Workflows run under the Kibana caller identity. The 11 Elasticsearch
  scope checks do not establish a separate credential per agent.
- AI contributes evidence interpretation and question answering. Not automated
  root-cause proof, and not autonomous remediation.

## Next

Integrate the verifier with an existing rollout controller — Argo Rollouts is the
obvious first target — and validate promotion, abort and pause behaviour against its
gates. Attribute existing telemetry by deployment revision so a contract can be written
against traffic a team already has. Add restart recovery and expiry handling. Give the
Investigator read-only deployment-diff and runbook tools so its diagnosis is worth more
than a restatement of the metrics.

Future work, not current capabilities.
