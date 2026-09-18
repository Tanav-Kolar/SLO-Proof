# SLOProof — evidence for recovery decisions

Submission draft, 18 September 2026. Scoped credentials and real trial evidence
are available. See [live validation](docs/LIVE_VALIDATION_20260918.md) for exact
measurements, encountered failures and operational limits.

Latest addition: [three explicit rollout outcomes](docs/ROLLOUT_CASES.md), a
guarded 10/25/50/100% runner, bounded evidence retries, and actual Slack escalation
inside `sloproof-loop`. All three native workflow branches were exercised on
recorded trials; Slack confirmed the failed-recovery alert. Fresh later-stage
EKS execution remains unvalidated because AWS authentication expired.

## Team

- Team name: tyr
- Project name: SLOProof
- Members: Vaibhav, Tanav, Shreyansh

## Problem

Platform and SRE teams need to establish whether a proposed recovery meets the
service's acceptance criteria. A reassuring AI explanation, a successful routing
change, or fewer errors alone does not establish that recovery. Missing telemetry
can also make a dashboard appear healthy.

The specific failure we set out to prevent is shipping a candidate that is
**better than the incident version but still broken** — the case where relative
improvement reads as success and the absolute target is quietly forgotten.

## Solution

We built an operator-supervised recovery-verification prototype. An operator
freezes a checkout service contract before a bounded routing trial. A separate,
read-only verifier queries Elasticsearch and returns PROVED, FAILED, or
INCONCLUSIVE with the exact evidence used. A controller records the result;
Bedrock agents investigate and explain the observations.

The contract requires at least 400 candidate requests in each of three
non-overlapping 60-second windows, success rate of at least 99%, and p95 latency
strictly below 300 ms. Completeness, reconciliation, configuration identity, and
observed routing are checked before a pass is possible.

PROVED means the observed trial met its contract. In an operator-started plan,
the runner can continue one guarded adjacent stage and measure again. The saved
10% pass does not establish full-load recovery. Verdict generation itself leaves
routing unchanged; there is no automatic reversal.

An agent may recommend a recovery. Expansion to more traffic requires predefined
checks against measured evidence, evaluated by a deterministic Go verifier that
no model can call, influence, or overrule.

## Architecture

- Data: EKS hosts a simulated Go checkout service in two configurations behind
  Envoy Gateway. A Kubernetes Job generates traffic from inside the cluster.
- Evidence: client-observed request outcomes are bulk-indexed into Elasticsearch;
  separate indices retain frozen contracts and the decision ledger.
- Verification: Go programs freeze contracts, conditionally patch one HTTPRoute,
  derive observed routing effectiveness, and evaluate saved ES|QL queries. The
  verifier checks its actual privileges and refuses an unrestricted credential.
- Reasoning: an Investigator recommends an action from observations, a Narrator
  explains an existing verdict, and a Briefer answers questions using saved query
  tools. They call Bedrock Converse and cannot execute Kubernetes changes.
- Elastic integration: Agent Builder provides an Investigator, Verdict Explainer
  and Evidence Briefer. A native Elastic Workflow retrieves the existing Go
  verdict, checks that exactly one decision exists, and sequences investigation
  and explanation. It branches on a matching deterministic rollout policy and
  sends configured Slack alerts; it has no routing or verdict-writing step.
- Presentation: an interactive “Would you ship this recovery?” walkthrough asks
  the audience to predict each result, then reveals the recorded Go verdict and
  native Elastic review. A full evidence view retains receipts and exact queries.
  The presentation works offline without credentials or traffic controls.

The demo uses generated traffic and injected faults. Both checkout variants run
the same image with different configuration. This is not an integration with an
existing production release pipeline.

## The result that matters

In `demo-slow-20260918-02` the candidate was **measurably better than the
incident version on both contract dimensions** and the gate still refused it:

| | Candidate (v1) | Incident version (v2) |
|---|---|---|
| Success rate | 100% | 80.00% |
| p95 latency | 507.6 ms | 857.6 ms (40.8% higher) |
| Verdict | **FAILED** — p95 must be strictly below 300 ms | — |

A system that compares a candidate against the thing it is replacing would have
called this a recovery. The frozen contract is absolute, so it returned FAILED,
kept routing unchanged, and stopped the rollout plan. The deterministic policy
additionally re-evaluated the incident version through the same window checks
and reported `BOTH_VERSIONS_BREACH` — explicitly as a hypothesis about a shared
cause, not a diagnosis, and without ordering traffic back to a worse version.

## What the retained evidence shows

- Scoped runtime separation: all 11 HTTP authorization checks passed.
- PROVED: `demo-healthy-20260918-01` had 600 candidate requests per window,
  100% success, and p95 57.23 / 57.28 / 57.34 ms across the three windows.
  All 29,993 completed records reconciled, with no drops or indexing failures.
- FAILED: `demo-slow-20260918-02` had 600 / 601 / 599 candidate requests in
  three windows, all successful, but p95 507.54 / 507.69 / 507.62 ms. All
  29,992 completed records reconciled, with no drops or indexing failures.
- INCONCLUSIVE: `demo-slow-20260918-01` lacked an observed-route receipt after
  conditional execution was refused. Missing evidence did not become a pass.
- The three Bedrock agents explain both measured trials with saved query
  citations. Their text remains advisory and requires review: retained early
  outputs include a truncated narrative, a count-denominator wording error and
  an overstatement of integrity-check scope.
- The native Elastic review workflow completed on the PROVED, FAILED and
  INCONCLUSIVE records, taking the `continue_planned_rollout`, `pause_rollout`
  and `hold_rollout` branches respectively. Execution
  `c92b3b29-0470-4121-b2b2-c20d07856658` delivered the failed-recovery alert to
  Slack (`ok: true`, timestamp `1789724091.666839`); executions
  `26d7ada9-dc4b-4e25-845d-57ca2dce83c7` and
  `c5c2799d-c23d-4903-aa05-e780efa0484a` are retained for the other two. A
  fourth execution with no recorded decision blocked review before any agent
  call. The INCONCLUSIVE run skipped metric investigation because it had no
  observation windows.

These results use generated traffic. No verdict automatically changes routing
or closes the incident. The earlier 57.48 ms latency check is shown separately
and is not relabeled as a recovery trial.

## Measured impact

Both versions were measured **concurrently, in the same 60-second windows, by
the same generator, through the same client-observed records**. This is a
side-by-side observation rather than a before/after comparison, so it is not
confounded by time, load, or infrastructure drift.

Across the three observation windows of `demo-healthy-20260918-01` (180 seconds):

| | Candidate (v1), 10% of traffic | Incident version (v2), 90% |
|---|---|---|
| Requests | 1,800 | 16,196 |
| Successes | 1,800 | 12,956 |
| **Failed requests** | **0** | **3,240** |
| Success rate | 100% | 80.00% |
| p95 latency | 57.2–57.3 ms | 857.6 ms |

At the measured ~90 requests/second the incident version was failing
**approximately 1,080 checkout requests per minute** while, in the same windows,
the trialled candidate served its share with zero failures at roughly one
fifteenth the p95 latency.

**Time from frozen contract to recorded verdict**, taken from the artifact
timestamps rather than estimated:

| Run | Freeze | Route effective | Verdict recorded | Elapsed |
|---|---|---|---|---|
| PROVED | 08:22:55 | 08:23:14 (+20 s) | 08:28:08 | **313 s** |
| FAILED | 08:16:40 | 08:16:55 (+15 s) | 08:21:50 | **310 s** |

That elapsed time is bounded by the contract's own three 60-second windows plus
its 30-second ingestion allowance and the generator's run length — not by human
triage. The decision arrives with its complete evidence attached.

Labelled assumptions and what we do not claim: the request rate, the fault
profiles and the 90/10 split are ours, generated for the demo, so the 1,080
failures per minute describes *this* injected incident and not any real service.
We have no revenue-per-checkout figure and make no revenue claim. We have no
comparable human-triage baseline and therefore claim no reduction in time to
recovery. A promoted candidate's behaviour at full production load is not
established by a 10% trial. The measurable outcomes we do claim are the
concurrent failure and latency differential above, contract compliance, and
the inspectability of every number behind both.

## Demo

Start with [the operator runbook](docs/DEMO_RUNBOOK.md) and
[control-plane instructions](src/control/README.md).

To view the saved evidence locally:

```bash
python3 scripts/render-evidence.py
python3 -m http.server 8768 --bind 127.0.0.1 --directory demo
```

Open `http://127.0.0.1:8768/`. Follow the
[four-minute showcase script](docs/SHOWCASE_SCRIPT.md); use “Full evidence” for
the audit view, and [`demo/process.html`](demo/process.html) to walk the full
sequence from contract freeze through bounded trial traffic, observation,
deterministic verification, advisory explanation and operator review. This is a
walkthrough of recorded results, not a live control dashboard. No recording or
public demo URL has been supplied yet.

Example query and agent question:

```bash
elastic/query.sh incident-timeline run_id=demo-1
scripts/agent.sh ask --run-id demo-1 \
  "Which version breached the latency target, and what evidence supports that?"
```

## Applying it to another service

The acceptance criteria are data, not code:
[`contracts/checkout-slo.json`](contracts/checkout-slo.json) holds the window
count and length, minimum samples, success and latency thresholds, eligible
traffic classes and freshness allowances. The verifier refuses to reinterpret a
contract it does not recognise, so changing a threshold is a reviewable diff
rather than a code change.

[The operator setup guide](docs/configure-a-service.html) walks through
configuring a different service: the evidence records a load generator must
emit, the index mappings, the scoped credential set, the route shape the
executor expects, and the queries the verifier runs. The initial customer is a
platform or SRE team already running Kubernetes with Gateway API and Elastic.

Reusable as-is: the contract schema, the three ES|QL evidence queries, the
credential separation model, the ledger shape, and the rollout policy. Service
specific: the checkout fault profiles, the two-backend HTTPRoute shape, and the
`v1 is the candidate` role convention.

## What we used

- Elastic: Elasticsearch Serverless, ES|QL, request/contract/ledger indices, and
  API-key privilege checks. Runtime credential separation is verified using
  separate ingest, verifier and controller keys.
- AWS: EKS, ECR, and Bedrock Converse; the agents default to GPT-OSS 120B.
- Agent Builder and Workflows: live post-verdict review and interactive evidence
  questions through Elastic's configured model connector, plus the Slack
  connector used by the failed-recovery branch. This is separate from the direct
  Bedrock path; the tested Kibana Bedrock connector could not accept the Workshop
  credentials' session token.
- Sarvam and SageMaker: not used in the delivered workflow.
- Other: Go, Python, Kubernetes Gateway API, Envoy Gateway, and static HTML.

## Validation

- `go vet ./...` clean and `go test -race ./...` passing in `src/control`,
  covering window evaluation, rollout progression guards and retry classification.
- Four regression tests for the bounded evidence-retry runner.
- 11 live Elasticsearch authorization checks, retained in
  `evidence/key-scopes.json`, plus a recorded refusal of an unrestricted
  credential presented as the verifier key.
- Every verdict artifact retains full ES|QL text, bound parameters, returned
  rows and exact window boundaries, so any number in this document can be re-run.

The AI contribution today is evidence interpretation and question answering, not
automated root-cause proof or autonomous remediation.

## Known limits

- Generated traffic and injected faults throughout; not an integration with an
  existing production release pipeline.
- Fresh 25 / 50 / 100% EKS stages are not live-validated; AWS authentication
  expired. The demonstrated PROVED evidence is the 10% candidate trial.
- Hashing gives traceability, not immutability. The controller holds ledger-write
  access and could forge records; this is not cryptographic attestation.
- No durable restart recovery, no independent expiry watchdog, no automatic
  reversal, and no autonomous incident initiation.
- The retained INCONCLUSIVE case is a missing receipt, not a demonstrated
  telemetry-loss or successful-retry case.
- Agent Builder and Workflows run under the Kibana caller identity; the 11
  Elasticsearch scope checks do not establish a separate credential per agent.

## What we would do next

Integrate the verifier with an existing rollout controller such as Argo Rollouts,
attribute existing telemetry by deployment revision, and validate promotion,
abort, and pause behavior. Add restart recovery and expiry handling, service
configuration beyond checkout, and semantic correctness checks. Give the
Investigator read-only deployment-diff and runbook tools to make its diagnosis
more useful. These are future work, not current capabilities.
