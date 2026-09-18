# Deterministic control plane

This code is implemented and locally tested. Scoped credentials pass all 11
authorization checks, and live trial results are retained in
[the validation report](../../docs/LIVE_VALIDATION_20260918.md).
Do not confuse unit-test fixtures or the earlier D9 latency measurement with
frozen-contract trials.

`cmd/verifier` is a separate read-only process. `scripts/control.sh verify`
launches it with an allowlisted environment containing only PATH, endpoint and
the verifier key. It checks actual Elasticsearch privileges and refuses an
unrestricted key even when that key is named `ELASTIC_VERIFIER_API_KEY`. It has
no ledger-writing command or Kubernetes credentials. It emits JSON; the controller
appends that JSON using its separate writer credential.

Implemented:

- Exact source-byte SHA256, create-only contract freezing per run, and saved-query
  hashes. A changed contract or query invalidates the trial.
- Route-only, one-hour-requested Kubernetes token in ignored
  `.kube/executor.config`; get/patch only on `checkout/checkout-route`.
- Conditional JSON Patch: UID, resourceVersion and complete ordinary rule tests,
  then two weight replacements. Conflicts append an ESCALATED receipt; there is
  no force retry. A durable intent prevents replaying a run's action.
- Both API acceptance and observed effective time in append-only receipts.
  Effective time is re-derived by the verifier from observed traffic.
- Three non-overlapping 60-second windows under the unchanged contract. Each
  queries completeness first, then metrics, then evidence integrity. Empty,
  unknown, changed, underpowered, premature or unreconciled evidence cannot pass.
- Full ES|QL text, bound parameters, result rows and exact window ranges in the
  verdict. A p95 equal to 300 ms fails (the criterion is strictly below).
- Generator completed/indexed counts reconciled against all searchable run
  records. Dropped arrivals or failed indexing cause INCONCLUSIVE. Duplicate
  detection does not use approximate COUNT_DISTINCT; request IDs are document IDs.

The convergence heuristic is explicit: three consecutive seconds with at least
40 observed requests each, no unknown version, and pooled v1 share within five
percentage points of requested routing. The anchor is the final observed request
in that cohort. This heuristic proves a measured cohort, not future routing.
The 30-second convergence allowance and all observation criteria are unchanged.

## Remaining limitations

The commands implement contract freeze → trial → observed receipt → verification
→ ledger append. Automatic promotion, reversal, durable end-to-end state-machine
recovery and an independent expiry watchdog are **not implemented**. Only the
10/90 trial is exposed by the controller CLI. There is no LLM invocation or agent
entry point. The operator still owns faults, setup, and reset.

Hashing provides traceability, not immutability against an operator with setup
credentials. The controller holds ledger-write access and could forge records;
this is not cryptographic attestation. Generator summaries come from operator-
collected Job logs. Full-run reconciliation assumes a unique run ID and a
completed Job. The ingest key intentionally has no refresh or read privileges;
its legacy self-reconciliation may print a warning, and the read-only verifier
performs the authoritative searchable-count reconciliation after the frozen lag.

## Running after provisioning scoped keys

Export credentials from the ignored `.env` in the operator shell. Scripts do
not discover or store AWS credentials. Keep the repository-pinned context:

```bash
export SLOPROOF_KUBE_CONTEXT=arn:aws:eks:us-west-2:994795733035:cluster/sloproof
elastic/create-api-keys.sh
elastic/verify-key-scopes.sh
scripts/setup-executor.sh
```

For each scenario use a fresh run ID. Set v1's operator-owned profile to `healthy`
or `bad-recovery`, wait for rollout, and put incident traffic on v2. Capture v1's
live `/version` response through a guarded port-forward, and use its `config_hash`
at freeze. Never change the profile during a frozen trial.

```bash
export RUN_ID=<unique-run-id>
scripts/control.sh freeze --run-id "$RUN_ID" --config-hash <live-v1-config-hash>
scripts/control.sh trial --run-id "$RUN_ID"
# LOADGEN_IMAGE must be a digest-pinned ECR image.
# Start promptly: the observed convergence cohort must fit within 30 seconds.
DURATION=240s WARMUP=0s RPS=100 scripts/run-loadgen.sh
# Once traffic has appeared in Elasticsearch:
scripts/control.sh observe --run-id "$RUN_ID"
# After the Job completes (which also covers window close + ingestion allowance):
scripts/verify-run.sh
```

Exit codes: verifier 0 = PROVED, 2 = FAILED or INCONCLUSIVE; configuration/IO
errors return 1. `record` appends the verdict to `sloproof-ledger`. `verify-run.sh`
collects the completed Job's actual log, invokes the separate verifier and records
its output. See the validation report for actual run IDs and measured outcomes.

Validation: `cd src/control && go test -race ./... && go vet ./...`.

## Operational meaning of a verdict

`recovery_assessment` is deterministic guidance, not an executable action.
Every result reports `incident_status: OPEN` and
`routing_action: NO_AUTOMATIC_CHANGE`. FAILED and INCONCLUSIVE block promotion;
PROVED is `ELIGIBLE_FOR_OPERATOR_REVIEW`, limited to the observed trial.
The ledger writer re-derives these fields rather than trusting supplied guidance.
A candidate can be better than the incident version while failing the contract;
that result must never automatically send traffic back to a worse version.

`scripts/run-trial.sh healthy|bad-recovery <unique-run-id>` implements one
operator-supervised scenario and `scripts/render-evidence.py` builds its read-only
presentation view. See `docs/DEMO_RUNBOOK.md` and the retained live validation.
