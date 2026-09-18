# Checkout service: features and configuration

The workload that fails as `checkout-v2` and recovers as `checkout-v1`. Everything
here is cloud-agnostic; AWS enters only at image push and cluster deploy.

## Design decision: one image, two configurations

`checkout-v1` and `checkout-v2` run the same binary. They differ only by
ConfigMap. This halves build time and makes the rejected-trial scenario a
one-line change.

**State this in the demo:** the recovery is therefore a *configuration*
rollback, not a code rollback. The routing proof is unaffected — traffic really
does move between two independently addressable versions — but do not let the
slides imply a code revert.

## Endpoints

| Endpoint | Purpose |
|---|---|
| `POST /checkout` | The only SLO-bearing path. Returns version, request ID, run ID, sequence, outcome, duration, and config hash; the same values appear as `x-sloproof-*` response headers so the load generator attributes outcomes without parsing bodies. |
| `GET /healthz` | Liveness. Stays green while a fault is active. |
| `GET /readyz` | Readiness. Fails immediately on SIGTERM so the pod leaves endpoints before connections stop. |
| `GET /version` | Provenance: version, fault profile, config hash, image digest, run ID, effective config. |

There is deliberately **no fault-toggle endpoint**. Faults are read from the
environment at process start. The recovery executor must not be able to cause
the incident it is trusted to repair.

## Fault profiles

Presets live in [`src/checkout/profiles.json`](../src/checkout/profiles.json) and
are selected with `FAULT_PROFILE`. Measured locally over 300 requests at 40
concurrent (`scripts/smoke-test-local.sh`):

| Profile | Injected | Measured success | Measured p95 | Role |
|---|---|---:|---:|---|
| `healthy` | none | 100% | 82 ms | v1 baseline; passes the contract |
| `checkout-regression` | +800 ms, 500 every 5th request | 80% | 878 ms | the v2 incident |
| `bad-recovery` | +450 ms | 100% | 541 ms | v1 latency breach → FAILED verdict run |

Contract thresholds are success ≥99% and p95 <300 ms, so `healthy` passes with
room and both fault profiles breach unambiguously.

Two properties are load-bearing:

- **Deterministic.** Errors come from an atomic request counter, not a RNG, and
  jitter is derived from the request sequence via splitmix64. A rerun of the
  same run reproduces the same distribution. Counters are per-pod and reset on
  restart — note that when reconciling a window that spans a rollout.
- **Scoped to `/checkout`.** Health probes are never faulted. If liveness tracked
  injected errors, Kubernetes would restart `checkout-v2` and delete the incident
  before it could be measured.

## Configuration: three separate layers

Collapsing these is what makes a recovery demo unconvincing.

| Layer | Owner | Where | Changed by |
|---|---|---|---|
| Service behaviour (fault profile) | Demo operator | ConfigMap → env | `scripts/set-profile.sh` (rollout; operator identity) |
| SLO acceptance criteria | Frozen pre-trial | [`contracts/checkout-slo.json`](../contracts/checkout-slo.json) | Nothing, once the run starts |
| Route weights | Recovery executor | HTTPRoute rule 2 | `scripts/set-weights.sh` (weights only) |

### Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `SERVICE_NAME` | `checkout` | Identity in responses and logs |
| `SERVICE_VERSION` | `v1` | Stamped on every response and record |
| `FAULT_PROFILE` | `healthy` | Selects a preset; unknown values exit non-zero at startup |
| `RUN_ID` | `unset` | Scopes a demo run for the verifier |
| `IMAGE_DIGEST` | `unset` | Echoed by `/version` for provenance |
| `PORT` | `8080` | |
| `BASE_LATENCY_MS` / `BASE_LATENCY_JITTER_MS` | from profile | Realistic healthy baseline — a 2 ms p95 makes the 300 ms threshold look unearned |
| `FAULT_LATENCY_MS` | from profile | Added to `/checkout` only |
| `FAULT_ERROR_EVERY_N` | from profile | `0` disables injection |
| `FAULT_ERROR_STATUS` | from profile | |
| `REQUEST_TIMEOUT_MS` | `3000` | Bounds handling; exceeding it returns 504 with outcome `timeout` |
| `READINESS_DELAY_MS` | `2000` | Readiness lags startup so rollouts do not admit cold pods |
| `SHUTDOWN_GRACE_MS` | `5000` | Drain window; must stay below `terminationGracePeriodSeconds` (30s) |

Precedence is explicit env over profile preset, so a one-off experiment never
requires editing `profiles.json`.

## Evidence features

- **Correlation passthrough.** `x-request-id`, `x-run-id`, and
  `x-sloproof-traffic-class` (`ordinary` | `synthetic` | `warmup`) are accepted
  and echoed into every record, so the ES|QL verifier excludes warmup and
  synthetic traffic by explicit field rather than by timestamp guesswork.
- **Config hash.** `/version` returns a 16-char fingerprint over the
  behaviour-relevant config. Snapshot it for both versions at contract-freeze
  time *and* at verdict time and store both in `sloproof-ledger`. This closes the
  obvious challenge — "how do we know v1 wasn't quietly made fast mid-trial?" —
  and the three profiles produce three distinct hashes.
- **Structured logs.** One JSON line per request with the same fields the load
  generator ships to `sloproof-requests`, for reconciliation. The verifier reads
  the client-observed index, because only that captures requests that never
  reached a pod.
- **Graceful drain.** Verified: on SIGTERM, `/readyz` returns 503 immediately
  while `/healthz` stays 200 and in-flight requests still succeed. Without this,
  every route weight change manufactures errors that look like application
  failure.

## Routing

[`k8s/41-httproute.yaml`](../k8s/41-httproute.yaml) has three rules. Rules 0 and
1 are operator-owned pinned routes selected by `x-sloproof-target: v1|v2`, for
synthetic journeys and preflight checks. Rule 2 is ordinary traffic and is the
only rule the executor may modify. `set-weights.sh` locates it by shape (the
single header-free rule) rather than by hardcoded index, and refuses if that is
ambiguous.

Every weight change writes a receipt to `evidence/` with route UID,
resourceVersion before and after, and the weights. `route_effective_at` is left
null on purpose: an accepted API patch is not evidence that traffic moved, so it
must be filled from observed served traffic.

## Safety: cluster guard

Every script that touches a cluster sources `scripts/_guard.sh`, which does two
things. It pins `KUBECONFIG` to `.kube/sloproof.config` inside the repo, so no
script ever reads the ambient kubeconfig; and it requires `SLOPROOF_KUBE_CONTEXT`
to match the context inside that file. This laptop's ambient contexts are
unrelated production clusters, so the isolation is not optional — with the pin in
place those contexts are not merely rejected, they are invisible to the scripts.

## Running it

Locally, no cluster and no cloud account:

```bash
scripts/smoke-test-local.sh
```

On EKS. Prerequisites: `brew install eksctl helm`, and an AWS profile with
permission to create a cluster. The scripts never read or store credentials.

```bash
export AWS_PROFILE=sloproof
export AWS_REGION=us-west-2

scripts/eks-up.sh                # eksctl create cluster, 15-20 min, billable
export SLOPROOF_KUBE_CONTEXT='<the context eks-up.sh prints>'

scripts/install-gateway.sh       # Envoy Gateway + Gateway API CRDs + GatewayClass
export GATEWAY_CLASS_NAME=eg

scripts/build-image.sh
scripts/push-ecr.sh              # creates the repo, prints a digest-pinned ref
export CHECKOUT_IMAGE='<account>.dkr.ecr.us-west-2.amazonaws.com/sloproof-checkout@sha256:...'

scripts/deploy.sh
```

Tear down when finished — the control plane, both nodes, and the NLB bill for as
long as they exist:

```bash
scripts/eks-down.sh              # releases load balancers before deleting the cluster
```

Demo sequence:

```bash
scripts/set-weights.sh 0 100     # incident: all traffic to faulty v2
scripts/set-weights.sh 10 90     # recovery trial: 10% back to healthy v1
scripts/set-weights.sh 100 0     # promotion (only on PROVED)
scripts/reset.sh                 # back to the starting state
```

## Not built here

Out of scope for *this layer*, though some of it now exists elsewhere in the
repo. Built since this document was first written: the load generator and its
`_bulk` shipper (`src/loadgen/`), the index templates and scoped-key setup
(`elastic/`), and the ES|QL window query. Still unbuilt: the **ES|QL verifier and
ledger writer** (`src/control/`), the Kibana view, and the Bedrock / Agent
Builder investigation path.

The contract file is the interface between this layer and the verifier, and the
verifier is the gap that matters — until it exists there is no verdict, only
measurements.
