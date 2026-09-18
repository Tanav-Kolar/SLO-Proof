# Canonical demo branch: main

Review on 18 September 2026. Use `main` for the integrated demo. Existing feature
branches remain available as history; they are not alternative deployment paths.

The later feature-branch update at `87fad88` was also reviewed. It rebased the
same tracing/Lambda change as `bdb8dbd`, added Elastic-side exports in `654cf89`,
and added local scratch/planning ignore rules. The ignore rules are integrated.
The exports are preserved under
[`docs/archive/elastic-agent-builder-20260918/`](archive/elastic-agent-builder-20260918/README.md)
as an explicitly archived prototype: its LLM-issued verdict, automatic rollback,
unbounded trace queries and API-time anchor violate the canonical demo boundary.
No active mapping, query or execution path is replaced by those definitions.

The final demo checkpoint also includes all three retained live trial outcomes,
both measured-trial agent briefings, the updated evidence export and operator
documentation, and the portable demo ZIP. A zero-byte route-file placeholder
from the refused first trial is omitted: it is not an execution receipt. The
original INCONCLUSIVE verdict and completed load-generator log are retained.

## Branch assessment

| Branch / change | Assessment | Integration decision |
|---|---|---|
| `main` at `e91f425` | Contains the deterministic Go verifier, conditional executor, scoped-key checks, in-cluster load generation, Bedrock advisory agents and recorded-evidence UI. This matches the operator-supervised demo boundary. | Keep as the base. |
| `feat/checkout-service-infra`: `8978889`, `762bedf`, `f87ea78` | Already present in main as `23cba70`, `d0ac0d1`, `89c96bf`. `git cherry` and `git range-diff` confirm equivalent patches. The local feature branch is simply behind its remote. | Preserve the existing main implementations; do not replace them with these older snapshots. |
| `feat/checkout-service-infra`: tracing part of `c2403d4` | Useful optional diagnostics and scratch-image CA trust. Original code always starts an exporter, requires an extra Secret at deployment, and describes a verifier dependency check that our contract does not implement. | Integrate with fixes: default off without an endpoint, optional named Secret keys, request/run/version correlation, and explicit simulated-dependency labels. |
| `feat/checkout-service-infra`: Lambda executor part of `c2403d4` | A competing execution path that does not enforce the main executor's guarantees. | Defer. Keep it in branch history; do not add its handler, deployment script or RBAC to the canonical demo. |
| `main`: missing-telemetry profile from `ea28343` | An explicitly injected evidence-loss scenario. The load generator still counts completed requests, so full reconciliation must reject withheld records. | Retain as a disclosed demo fault. It is not a natural outage or a second source of SLO evidence. Existing running image digests are not changed by this consolidation. |

## Why the Lambda executor is deferred

Concrete code findings in `c2403d4:infra/lambda/executor/handler.py`:

1. `_handle_execute` accepts arbitrary backend names from `weights` and replaces
   the entire `backendRefs` array with port 80. It can change identities, not just
   the two existing weights. It does not condition its patch on UID,
   resourceVersion, expected backend identities or expected current weights.
2. `_read_rollback_token` decodes unsigned base64 JSON. `_handle_rollback` does
   not validate the recorded route UID, resourceVersion, current weights or token
   expiry before applying the supplied backend state.
3. The supplied `contract_uid` is echoed, not validated against a frozen
   contract. The receipt contains API acceptance time but no traffic-derived
   effective timestamp. The caller cannot use it as our observation anchor.
4. Missing `EXECUTOR_SHARED_SECRET` bypasses the handler's authentication check;
   the deploy script configures an unauthenticated Function URL with an
   application-level shared secret. This is an unnecessary second access path
   for the current demo, and the branch reports the connector URL still returns
   403 while direct Lambda invocation worked.

These findings are source review. This consolidation does not invoke, deploy,
remove or change permissions for any live Lambda. Previously provisioned cloud
resources, if any, require a separate runtime inventory; merging code does not
prove those resources are disabled.

## Boundaries after integration

- `scripts/run-trial.sh` and `src/control/` remain the canonical workflow.
- A verdict does not automatically promote, reverse or close an incident.
- OpenTelemetry spans are diagnostic. The verifier still reads client-observed
  request documents under the unchanged frozen SLO contract.
- The `checkout -> quote` span is an in-process simulation. There is no separate
  quote service, and this does not establish independent dependency health.
- Tracing credentials are optional and can populate only the named OTel
  environment variables; they cannot override service identity or fault config.
- No new service image or manifest is deployed during an ongoing frozen trial.

## Validation

Checkout tests cover no-exporter startup without an endpoint and preservation of
HTTP outcomes, correlation fields and explicit simulation labeling when tracing
is enabled. Run `go test -race ./...`, `go vet ./...`, and `go mod verify` in
`src/checkout`; rerun the control-plane tests and existing local smoke test before
pushing the integrated branch. Trace ingestion into Elastic is not claimed by
these local tests.

The original smoke script could accidentally query an unrelated process on its
fixed ports and exit successfully without checking the values. It now uses fresh
ports, checks the responding run/profile/version, and asserts expected outcomes.
After that fix, the local integrated build measured:

| Local profile | Success | p95 | Expected behavior |
|---|---:|---:|---|
| healthy | 100% | 55.3 ms | passed |
| checkout-regression | 80% | 855.6 ms | passed |
| bad-recovery | 100% | 505.5 ms | passed |

These are local smoke measurements, not Elasticsearch recovery verdicts.
