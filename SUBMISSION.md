w# SLOProof — evidence for recovery decisions

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

Nothing about that is specific to checkout. Any service whose recovery can be stated
as a threshold over client-observed requests has this problem, and checkout is our
demonstration vehicle rather than our scope — see
[onboarding another service](#onboarding-another-service) for what transfers and what
is still pinned to the demo.

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

## Demo

```bash
python3 scripts/render-evidence.py
python3 -m http.server 8768 --bind 127.0.0.1 --directory demo
```
Pitch deck link : https://docs.google.com/presentation/d/1nGVhCJGwVdq1QCInwFuHHVC6mmjR8qny/edit?usp=sharing&ouid=118334502760775484256&rtpof=true&sd=true


`http://127.0.0.1:8768/` opens the decision challenge: predict each verdict, then see
the recorded one. It opens by asking whether you would ship a candidate with 100%
successful checkouts; the answer is no, for the reason in "The result that matters"
above. Offline export, no credentials.

Running it live, the presenter script and the stage-by-stage process view are all in
[the operator runbook](docs/DEMO_RUNBOOK.md),
[the showcase script](docs/SHOWCASE_SCRIPT.md) and
[`demo/process.html`](demo/process.html).

## Onboarding another service

The decision machinery is service-agnostic by construction. The deployment adapter
around it is not yet, and the boundary is worth stating exactly, because "works for
any service" is the sort of claim that should come with a file list.

**Transfers unchanged.** The contract schema — window count and length, minimum
samples, success and latency thresholds, eligible traffic classes, freshness
allowances. The evidence document shape. The three-key credential separation. The
ledger, the rollout policy, and the 10/25/50/100 staging. Window evaluation,
reconciliation, the observed-share check, and INCONCLUSIVE semantics. The Elastic
tools, agents and `sloproof-loop` workflow, which are parameterised by run ID and
never mention checkout. None of that reasons about what the service does — only
about whether its measured requests met a frozen threshold.

**A new service supplies** a client vantage that emits the request document, two
deployable versions behind one weighted route, its own thresholds, and an
index-visibility measurement to replace the provisional 30-second ingestion
allowance. [The operator setup guide](docs/configure-a-service.html) walks each of
those.

**Still pinned to the demo**, and enumerable:

| What | Where |
|---|---|
| Contract must declare `checkout` / `/checkout` | `src/control/contract.go:62` |
| Route `checkout-route`, namespace `checkout`, path `/checkout` | `src/control/executor.go:35,56` |
| Backends named `checkout-v1` / `checkout-v2` | `src/control/executor.go:84` |
| Integrity query filters `service != "checkout"` | `src/control/verifier.go:62` |
| The convention that v1 is the candidate | throughout |

Those are literals, not assumptions baked into the logic — the contract already
carries `service` and `endpoint` as data, the validator just compares them to
constants instead of reading them. Unpinning them is a bounded change, and the
honest status today is the one our own guide gives: service reuse is documented,
not automated. We would rather say that than claim a generality a judge can
disprove by opening one file.

The initial customer is a platform or SRE team already on Kubernetes with Gateway
API and Elastic.

## Stack

- **Elastic** — Elasticsearch Serverless, ES|QL, separate request/contract/ledger
  indices, API-key privilege checks, Agent Builder, Workflows, Slack connector.
- **AWS** — EKS, ECR, Bedrock Converse for the standalone agent path.
- **Other** — Go, Python, Kubernetes Gateway API, Envoy Gateway, OpenTelemetry,
  static HTML.

Agent Builder uses Elastic's configured model connector, which is a different path
from the direct Bedrock agents; the Kibana Bedrock connector could not accept the
workshop credentials' session token. Sarvam and SageMaker are not used.

**How the pieces fit.** Elasticsearch holds client-observed requests, frozen
contracts and the decision ledger in three indices under three separate API keys.
Every contract check is ES|QL, and the contract hashes the exact query *text*, not
just the thresholds — which is why re-running a verdict hours later reproduces it
byte for byte. Agent Builder's Investigator, Verdict Explainer and Evidence Briefer
are each restricted to three read-only, bounded tools. The `sloproof-loop` workflow
sequences the review and has no routing step and no verdict-writing step.

**The gate is not the model.** The workflow reads the recorded Go verdict, requires
exactly one, branches on the matching deterministic policy, alerts Slack, and only
then calls an agent. A run with no recorded decision stops the review before any
agent call — kept as evidence: `REVIEW_BLOCKED … 0 agent calls`. Expansion past the
current stage needs a fresh frozen trial and new measurements, never an agent's
recommendation. All three outcome branches alerted live to
`#all-on-call-engineering-team`: PROVED `ts 1789728283.725569`, FAILED
`ts 1789726542.717359`, INCONCLUSIVE `ts 1789726104.948059`, each carrying the
per-window numbers and the contract hash, none carrying model output.

Server-side OpenTelemetry spans are collected and deliberately excluded from the
verdict. In one window of `live-bad-recovery-20260918-01` the client observed 5,817
failed requests that produced no spans at all, having been rejected before reaching
an instrumented pod, while the instrumented cohorts matched the client counts
exactly. Spans are advisory context — `elastic/queries/otel-*.esql` and the
`sloproof-otel-spans` dashboard sit them beside the contract evidence — and the
verifier's scoped credential is refused on that data with HTTP 403.
