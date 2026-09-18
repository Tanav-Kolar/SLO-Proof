# Live validation — 18 September 2026

These are retained measurements from generated checkout traffic on EKS, not
production customer traffic or unit-test fixtures. The account is `994795733035`,
cluster `sloproof`, region `us-west-2`. Both versions use the same service image
with different fault configurations. v1 is the recovery candidate; v2 is the
incident version.

## Runtime separation

All 11 HTTP authorization checks passed using the three scoped Elasticsearch
keys. The verifier can read requests, contracts and ledger but cannot write
them; ingest can create requests but cannot write the ledger; the controller
can read requests and write the ledger but cannot write request evidence.
See [sanitized checks](../evidence/key-scopes.json). No key material is included.

## Recorded trials

| Run | Result | Actual evidence |
|---|---|---|
| `demo-slow-20260918-01` | INCONCLUSIVE | Conditional execution was refused; no observed-route receipt existed. This is missing execution evidence, not an SLO breach. |
| `demo-slow-20260918-02` | FAILED | Three candidate windows: 600 / 601 / 599 requests, all 100% successful; p95 507.5396 / 507.694 / 507.6235 ms. |
| `demo-healthy-20260918-01` | PROVED | Three candidate windows: 600 requests each, all 100% successful; p95 57.2282 / 57.28355 / 57.3444 ms. |

The slow trial reconciled all 29,992 completed requests against Elasticsearch,
with no dropped arrivals or indexing failures. Its observed effective time was
`2026-09-18T08:16:55.99Z`. Each of the three non-overlapping windows lasts 60
seconds. The unchanged contract requires at least 400 candidate requests per
window, success at least 99%, and p95 strictly below 300 ms.

The healthy trial reconciled all 29,993 completed records with no dropped
arrivals or indexing failures. Its observed effective time was
`2026-09-18T08:23:14.99Z`; verification completed at `08:28:08.548229Z`.

The contract SHA256 is
`dd9fdf5a9e7e540d790a591ee2a9f89844e54f5228419cfdf88d4cad32f3c63f`.
Every verdict retains exact ES|QL, bound parameters, result rows, receipts and
reconciliation. Evidence is under `evidence/<run-id>-verdict.json`.

Every verdict reports incident OPEN and NO_AUTOMATIC_CHANGE. FAILED and
INCONCLUSIVE block promotion. The recorded PROVED result applies only to the
observed trial and permits operator review; it is not full-load recovery.

## Blockers encountered and retained

- The first route comparison rejected semantically identical JSON because raw
  JSON formatting differed between the frozen record and Kubernetes response.
  The executor now compares decoded JSON while preserving UID, resourceVersion,
  spec and conditional patch checks. The original refused run was not retried
  or relabeled. Formatting and key-order regressions are covered by Go tests.
- Ingest intentionally cannot refresh or count the index. Its self-check reports
  403 and unavailable search count; the independent read-only verifier performs
  authoritative reconciliation. No permission was broadened to hide this.
- An early Investigator explanation mislabeled version roles. Explicit candidate
  and incident roles were added to prompts; the earlier advisory remains in the
  ledger. AI explanations are fallible and do not determine verdicts.
- A Briefer call without an explicit window returned no citations and was
  rejected. The retained briefing command supplies the actual verifier window.
- A slow-trial narrative was cut short. The earlier local artifact is retained;
  subsequent narratives require an `end_turn` stop reason. The evidence export
  displays the latest saved attempt and lists earlier artifacts.
- The second slow-trial narrative says failures "out of" the success count.
  That wording is incorrect: failed and successful outcomes are separate counts,
  not a numerator and denominator. The raw answer is retained, not silently
  corrected. Query citations do not validate all generated prose.
- The healthy narrative describes integrity checks as covering the entire run;
  those checks cover the observation windows. Full-run record-count reconciliation
  is a separate check. Treat the deterministic query ranges as authoritative.

## Reproduce and present

Follow [the operator runbook](DEMO_RUNBOOK.md). Use unique run IDs; do not replay
an existing action. After a recorded verdict with observation windows:

```bash
python3 scripts/brief-trial.py <run-id>
# Additional briefing, retaining the original:
python3 scripts/brief-trial.py <run-id> --attempt 2
python3 scripts/render-evidence.py
```

Briefings contain real Bedrock Investigator, Narrator and Briefer output. The
Investigator is advisory, the Narrator explains a recorded deterministic verdict,
and the Briefer calls saved read-only query tools. These calls are post-trial
explanations, not proof that an agent initiated or executed recovery.

All three agents completed on both measured trials, with Investigator schema/
measurement validation passing and Briefer query citations present. The healthy
briefing is `evidence/demo-healthy-20260918-01-agents.json`; the latest slow-trial
briefing is `evidence/demo-slow-20260918-02-attempt-2-agents.json`.

## Validation and limits

### Subsequent Elastic integration

Active definitions are in [`elastic/`](../elastic/README.md). Native Workflow
`sloproof-loop` completed with the original Go decisions unchanged:

| Recorded run | Go verdict | Elastic execution ID | Review behavior |
|---|---|---|---|
| `demo-slow-20260918-02` | FAILED | `75d29ba0-5c03-43fd-9f0c-0b3033113054` | Retrieved the decision, investigated the first actual window, explained the fixed failure. |
| `demo-healthy-20260918-01` | PROVED | `95a999fd-72f3-4b40-acbb-fa3ef5ce2e50` | Retrieved the decision, investigated the window, explained trial-only scope. |
| `demo-slow-20260918-01` | INCONCLUSIVE | `cd0e1ae9-f540-4bfe-99e1-77de1a7951ad` | Skipped metric investigation because no observation windows existed; explained missing execution evidence. |
| `integration-missing-decision-20260918` | No SLO verdict | `ba6035eb-beed-4642-853b-a02cf00bf86d` | REVIEW_BLOCKED, zero agent calls. This is an actual negative integration check, not a recovery trial. |

Saved `*-elastic-tools.json` responses match the two measured trials' request
counts, success rates, p95 and recorded decision. The exact canonical metric and
completeness query text is used, with explicit run and window parameters.
`*-elastic-review.json` retains workflow steps, actual agent output and source
decision identity. These are fresh reviews of earlier trial records, not fresh
traffic trials. No cluster or route change was made during integration.

The first live Briefer answer lacked the actual frozen thresholds in its input
and overstated the scope of its completeness query. It is retained as answer 1.
The command now retrieves the frozen contract with the read-only key, verifies
its source hash/body against the recorded decision, and supplies the exact
criteria and completeness limits. Later answers remain advisory.
Answer 2 still described the strict p95 threshold as inclusive and overstated
coverage. It is retained. Answer 3 includes the explicit strict inequality and
the limits of the completeness tool; its retrieved counts, rates, p95 and
recorded verdict match the evidence. The export shows the latest answer and
lists both earlier artifacts. This inspection is not a guarantee that arbitrary
future AI answers will be correct.

Agent Builder execution metadata reports Elastic's configured hosted connector;
the Bedrock Python path remains separate. Kibana uses its caller/workflow
identity, not the Go verifier's scoped key. Read-only tools and steps constrain
this integration's interfaces; the earlier 11 key checks do not establish a
per-agent Kibana credential boundary. The original route-changing workflow was
replaced by this manual review flow; the disabled rule and redacted connectors
are configuration references and were not activated or invoked.

### Remaining limits

Thirteen local integration boundary tests pass. Live validation covers three
recorded verdict reviews, the missing-decision gate with zero agent calls, and
the three Agent Builder tools against both measured trials. The integrated HTML
was checked for JavaScript syntax and inspected in the browser.

`go test -race ./...` and `go vet ./...` passed in `src/control`.
Python compilation and mocked complete/incomplete narrative checks passed.
There is no automatic promotion, reversal, durable orchestration restart recovery
or independent expiry watchdog. General service reuse, Argo integration, semantic
correctness probes, full-load verification and matched business-impact baselines
remain future work. The cluster is left deployed for the demonstration and
continues billing until explicitly torn down. A final read-only cluster check
confirmed all load Jobs completed and ordinary traffic remained v1=10 / v2=90.
