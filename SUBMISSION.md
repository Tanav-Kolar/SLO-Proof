# SLOProof — evidence for recovery decisions

Submission draft, 18 September 2026. Scoped credentials and real trial evidence
are available. See [live validation](docs/LIVE_VALIDATION_20260918.md) for exact
measurements, encountered failures and operational limits.

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
- Elastic integration: Agent Builder provides an Investigator, Verdict Explainer
  and Evidence Briefer. A native Elastic Workflow retrieves the existing Go
  verdict, checks that exactly one decision exists, and sequences investigation
  and explanation. It has no routing or verdict-writing step.
- Presentation: an interactive “Would you ship this recovery?” walkthrough asks
  the audience to predict each result, then reveals the recorded Go verdict and
  native Elastic review. A full evidence view retains receipts and exact queries.
  The presentation works offline without credentials or traffic controls.

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

Open `http://127.0.0.1:8768/`. Follow the
[four-minute showcase script](docs/SHOWCASE_SCRIPT.md); use “Full evidence” for
the audit view. This is a walkthrough of recorded results, not a live control
dashboard. No recording or public demo URL has been supplied yet.

Demonstrated with retained evidence:

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
  INCONCLUSIVE records. The INCONCLUSIVE run skipped metric investigation because
  it had no observation windows. A fourth execution with no recorded decision
  blocked review before any agent call. Agent Builder tools matched the saved
  healthy and slow trial evidence; execution IDs and outputs are retained.

These results use generated traffic. No verdict automatically changes routing
or closes the incident. The earlier 57.48 ms latency check is shown separately
and is not relabeled as a recovery trial.

Example query and agent question:

```bash
elastic/query.sh incident-timeline run_id=demo-1
scripts/agent.sh ask --run-id demo-1 \
  "Which version breached the latency target, and what evidence supports that?"
```

## What we used

- Elastic: Elasticsearch Serverless, ES|QL, request/contract/ledger indices, and
  API-key privilege checks. Runtime credential separation is verified using
  separate ingest, verifier and controller keys.
- AWS: EKS, ECR, and Bedrock Converse; the agents default to GPT-OSS 120B.
- Agent Builder and Workflows: live post-verdict review and interactive evidence
  questions through Elastic's configured model connector. This is separate from
  the direct Bedrock path; the tested Kibana Bedrock connector could not accept
  the Workshop credentials' session token.
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
