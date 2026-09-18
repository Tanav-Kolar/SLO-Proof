# SLOProof: operator-supervised recovery verification

## What this demo can claim

A deterministic, read-only verifier checks client-observed results against a
contract frozen before the trial routing change. It returns PROVED, FAILED or
INCONCLUSIVE with the exact queries and time ranges. An AI explains evidence;
an operator decides what routing action to take.

A failed trial does **not** trigger an automatic reversal. The incident version
may be worse than the candidate. A candidate can improve the incident and still
miss the acceptance criteria. Every trial verdict leaves the incident open and
routing unchanged. Even PROVED applies only to the observed trial load.

## Current gate

Healthy in-cluster v1 has a measured p95 of 57.48 ms across 7,001 ordinary
requests, with 8,000 total records reconciled. This was a measurement check,
not a completed frozen-contract recovery trial.

Scoped Elasticsearch credentials are still required before the real FAILED and
PROVED scenarios can be demonstrated. `demo/evidence.html` clearly displays this
absence; it does not insert fixture verdicts.

## Operator commands once scoped credentials are ready

From the repository root, supply environment variables in the operator shell:

```bash
set -a
source .env
set +a
export SLOPROOF_KUBE_CONTEXT=arn:aws:eks:us-west-2:994795733035:cluster/sloproof
export LOADGEN_IMAGE=994795733035.dkr.ecr.us-west-2.amazonaws.com/sloproof-loadgen@sha256:8e594bf5786ceb4e92a14baac4008490db6e2930a05b23bb64554a3107cfdd28
elastic/verify-key-scopes.sh
```

Use unique run IDs. Run one scenario at a time:

```bash
scripts/run-trial.sh bad-recovery demo-insufficient-01
# Exit 2 is expected for a real FAILED or INCONCLUSIVE result; inspect the reason.
# At this point routing remains 10/90. There is no automatic reversal.

# Starting the next scenario is an explicit OPERATOR setup action:
# it changes the profile and establishes a fresh incident baseline before freeze.
scripts/run-trial.sh healthy demo-healthy-01
```

Each scenario checks required credentials before mutation, verifies the read
key's actual privileges, refuses concurrent load jobs and reused runs, prepares
profiles, records live `/version` responses for every ready candidate pod,
starts in-cluster traffic, freezes the contract, performs a conditional 10/90
trial patch, collects five minutes of evidence, derives the effective timestamp,
verifies three 60-second windows, and appends the verdict. It leaves routing
unchanged on success, failure, or insufficient evidence.

The runner requests a fresh route-only executor token. It never supplies AWS,
Kubernetes or controller keys to the verifier subprocess. A conflict or partial
failure may require operator attention; the runner does not force past it.

The ingest key's built-in refresh/count check can be forbidden by design.
`indexed_ok` and `completed` from the finished Job are reconciled by the read-only
verifier against Elasticsearch after the contract's ingestion allowance.

The full scenario orchestration has not yet been exercised with scoped keys.
Treat it as implemented but awaiting live validation.

## Presentation screen

```bash
python3 scripts/render-evidence.py
python3 -m http.server 8768 --bind 127.0.0.1 --directory demo
```

Open `http://127.0.0.1:8768/evidence.html`. It is a portable export: no API keys,
network queries or execution controls are embedded. Refresh the page after
regenerating. The page includes only real saved measurements and explicit
verifier outputs from `evidence/*-verdict.json`; do not put test fixtures there.

For each trial it shows the candidate's metrics and v2's concurrent metrics in
each observation window. Percentiles remain per-window; they are never averaged
into a misleading run-wide percentile. Missing evidence is displayed as
unavailable, not zero latency.

## Four-minute narration after the live gates pass

1. **Problem (30 seconds):** “When an intervention looks promising, how do we
   know it has actually met the recovery target?” Show the frozen contract.
2. **Incident and advice (30 seconds):** Show measured v2 errors and latency.
   Show a real investigator recommendation and its cited query IDs. The agent
   is advisory and does not get executor access.
3. **Insufficient recovery (60 seconds):** Open the actual bad-recovery verdict.
   “This candidate improves errors, but checkout is still too slow. FAILED means
   the recovery target is unmet. It does not tell us to return to a worse
   version.” Show routing unchanged and incident OPEN.
4. **Healthy trial (60 seconds):** Open the actual healthy verdict and its three
   windows. “This trial meets the same frozen contract. An operator can now
   review expansion; we have not claimed full-load recovery.”
5. **Boundary (45 seconds):** Show the actual scope checks and missing-evidence
   behavior. Open a query and receipt. No invented verdicts or hidden fixtures.
6. **Close (15 seconds):** “The outcome is an inspectable recovery decision:
   contract, measured results, and receipt.”

Show a recorded five-minute execution or its retained evidence instead of
waiting through the observation windows on stage. Label recordings and exports.
No avoided-revenue or prevented-failure claims without a matched comparison.

## What to leave out

No new cloud integrations, voice, additional agents, autonomous promotion,
automatic rollback, or claims that this is a complete incident-resolution
system. The working product boundary is operator-supervised verification.

After recording and final demonstration, tear down through the guarded
`scripts/eks-down.sh`; the cluster and load balancer still bill until removed.
