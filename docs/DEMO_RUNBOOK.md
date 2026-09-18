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

## Retained live evidence

All 11 scoped-credential authorization checks passed. Actual trial run IDs,
per-window measurements, encountered failures and limits are recorded in
[live validation](LIVE_VALIDATION_20260918.md). The HTML export uses these saved
artifacts, not fixture verdicts.

Present `demo-slow-20260918-02` (FAILED at roughly 508 ms), then
`demo-healthy-20260918-01` (PROVED at roughly 57 ms), and finally
`demo-slow-20260918-01` (INCONCLUSIVE: missing observed-route receipt).
Both measured trials used the same acceptance contract. No automatic expansion
occurred after the pass.

## Operator commands

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

Scoped-key orchestration has been exercised live. Re-running starts new cloud
workloads and explicitly changes the incident baseline; it is not needed to
present the retained evidence.

For an AI explanation of a recorded verdict with observation windows:

```bash
python3 scripts/brief-trial.py <run-id>
# Use --attempt 2 for an additional briefing; earlier output is retained.
```

This invokes three advisory Bedrock agents after verification. It does not show
an agent autonomously initiating or executing recovery.

## Elastic Agent Builder and Workflows

Active definitions and commands are under [`elastic/`](../elastic/README.md).
Show `sloproof-loop` in Kibana Workflows: read the Go verdict, require exactly
one decision, investigate its bounded evidence, and explain the fixed result.
An INCONCLUSIVE run without windows skips metrics. A missing decision blocks
the review before any agent is called.

```bash
.venv/bin/python elastic/manage.py run demo-slow-20260918-02 --attempt 2
.venv/bin/python elastic/manage.py collect demo-slow-20260918-02 --attempt 2
python3 scripts/render-evidence.py
```

The workflow runs asynchronously; collect after completion. No fresh recovery
trial is needed to demonstrate this review. `manage.py ask` invokes the Evidence
Briefer with actual time bounds. This uses Elastic's configured model connector;
the separately displayed Python agents use Bedrock. There are no routing,
ledger-write or Slack steps.

## Presentation screen

```bash
python3 scripts/render-evidence.py
python3 -m http.server 8768 --bind 127.0.0.1 --directory demo
```

Open `http://127.0.0.1:8768/` for the guided decision challenge. Follow
[the showcase script](SHOWCASE_SCRIPT.md) or open “Presenter cues” on the page.
Ask a judge to predict the result, reveal the recorded verdict, then open the
actual saved Elastic review. The reveal controls change only the presentation.
“Full evidence” opens `evidence.html` for deeper questions.

These are portable exports: no API keys,
network queries or execution controls are embedded. Refresh the page after
regenerating. The page includes only real saved measurements and explicit
verifier outputs from `evidence/*-verdict.json`; do not put test fixtures there.

For each trial it shows the candidate's metrics and v2's concurrent metrics in
each observation window. Percentiles remain per-window; they are never averaged
into a misleading run-wide percentile. Missing evidence is displayed as
unavailable, not zero latency.

## Alternative narration without audience participation

The preferred interactive four-minute sequence is in
[SHOWCASE_SCRIPT.md](SHOWCASE_SCRIPT.md). If the format does not allow audience
participation, use this direct walkthrough:

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
