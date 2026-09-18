# SLOProof — evidence for recovery decisions

Submission draft, 18 September 2026. Live recovery verdicts remain pending scoped
Elasticsearch credentials. Update the demo status only after the retained
evidence shows the actual outcome.

## Team

- Team name: tyr
- Project name: SLOProof
- Members: team to supply names before submission.

## Problem

Platform and SRE teams need to establish whether a proposed recovery meets the
service's acceptance criteria. A reassuring AI explanation, a successful routing
change, or fewer errors alone does not establish that recovery. Missing telemetry
can also make a dashboard appear healthy.

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

PROVED means the observed trial met its contract and is eligible for operator
review. It does not establish full-load recovery. Every verdict leaves routing
unchanged: this prototype does not autonomously promote or reverse traffic.

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
- Presentation: a portable HTML export shows saved measurements, verdicts,
  receipts, and exact queries without embedding credentials.

The demo uses generated traffic and injected faults. Both checkout variants run
the same image with different configuration. This is not an integration with an
existing production release pipeline.

## Demo

Start with [the operator runbook](docs/DEMO_RUNBOOK.md) and
[control-plane instructions](src/control/README.md).

To view the saved evidence locally:

```bash
python3 scripts/render-evidence.py
python3 -m http.server 8768 --bind 127.0.0.1 --directory demo
```

Open `http://127.0.0.1:8768/evidence.html`. This is an evidence export, not a live
control dashboard. No recording or public demo URL has been supplied yet.

Measured so far:

- In-cluster healthy v1: 7,001 ordinary requests, 100% success, 57.48 ms p95.
- All 8,000 attempts including warmup reconciled, with zero dropped arrivals and
  zero indexing failures. This was a latency check, not a frozen-contract trial.
- A negative credential check returned INCONCLUSIVE when the verifier received
  an unrestricted key. This is a credential refusal, not an SLO failure.
- The Investigator and Briefer have answered against real historical evidence;
  the Narrator refuses a run with no recorded verdict.

Still to demonstrate: a healthy PROVED trial, a slow-candidate FAILED trial,
and insufficient-evidence behavior with scoped runtime credentials. These are
not represented as complete in the evidence export.

Example query and agent question:

```bash
elastic/query.sh incident-timeline run_id=demo-1
scripts/agent.sh ask --run-id demo-1 \
  "Which version breached the latency target, and what evidence supports that?"
```

## What we used

- Elastic: Elasticsearch Serverless, ES|QL, request/contract/ledger indices, and
  API-key privilege checks. Runtime credential separation awaits scoped keys.
- AWS: EKS, ECR, and Bedrock Converse; the agents default to GPT-OSS 120B.
- Agent Builder: previously probed successfully, but the current reasoning path
  calls Bedrock directly because the tested connector could not accept the
  Workshop credentials' session token.
- Sarvam and SageMaker: not used in the delivered workflow.
- Other: Go, Python, Kubernetes Gateway API, Envoy Gateway, and static HTML.

The measurable outcome is trial compliance plus inspectable evidence. We do not
claim revenue saved, prevented customer failures, or reduced recovery time without
a comparable baseline. The AI contribution today is evidence interpretation and
question answering, not automated root-cause proof or autonomous remediation.

## What we would do next

Integrate the verifier with an existing rollout controller such as Argo Rollouts,
attribute existing telemetry by deployment revision, and validate promotion,
abort, and pause behavior. Add restart recovery and expiry handling, service
configuration beyond checkout, and semantic correctness checks. Give the
Investigator read-only deployment-diff and runbook tools to make its diagnosis
more useful. These are future work, not current capabilities.
