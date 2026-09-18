# Handoff prompt — SLOProof, next steps

Paste everything below the line into a fresh model session. It assumes no prior
knowledge of this repository. Written 18 September 2026 after the reasoning
plane landed; re-verify the state table before trusting it, because several of
these facts have already flipped once.

---

You are picking up a hackathon project mid-build. Read this brief, then work the
critical path in the order given. Verify each claim in the state table before
relying on it — some were true yesterday and are not true now, and the project's
whole argument is that measured evidence beats asserted state.

## What the project claims

SLOProof demonstrates that a service recovery can be **proved rather than
asserted**. A checkout service runs in two versions behind weighted routing. v2
carries an injected regression. The operator shifts a slice of traffic to
healthy v1, and a **deterministic verifier** — not a human, not an LLM — reads
client-observed request records out of Elasticsearch, evaluates them against a
contract frozen before the trial began, and returns PROVED, FAILED or
INCONCLUSIVE.

The core claim is **separation**: the thing that decides whether to promote must
be computationally and credentially separate from the thing that wants to be
promoted. Anything that weakens that separation weakens the entire submission.
An LLM must never make the decision.

Repository root: `/Users/weekday/Documents/ChatGPT/sloproof/forge-tyr`

Read first, in this order:
- `docs/SERVICE_ARCHITECTURE.md` — the four planes, decisions D1–D9, trust
  boundaries, and a live capability baseline. Section 8 has current status.
- `docs/CHECKOUT_SERVICE_SPEC.md` — endpoints, fault profiles, config layers
- `contracts/checkout-slo.json` — the acceptance criteria the verifier enforces
- `src/agents/README.md` — the reasoning plane and why it bypasses Agent Builder

## State table — verify, do not trust

| Thing | State | Evidence |
|---|---|---|
| EKS cluster `sloproof` | **ACTIVE**, k8s 1.34, account `994795733035`, created 06:15Z today | `aws eks describe-cluster --name sloproof --region us-west-2` |
| **Cluster access from this repo** | **BROKEN — fix first** | `.kube/sloproof.config` names `arn:aws:eks:us-west-2:213325652666:cluster/sloproof` and execs `aws --profile sloproof`, which does not exist. Wrong account, missing profile |
| checkout v1 / v2, Envoy Gateway, HTTPRoute | deployed and previously serving | `k8s/*.yaml`, all applied |
| Weighted routing | **proved** at exactly 10.0 / 90.0 across 14,689 requests | `elastic/queries/traffic-split.esql` on `run_id=demo-1` |
| Elasticsearch evidence | 37,999 request records ingested, mappings correct, ES\|QL verified | `scripts/agent.sh preflight` |
| Load generator | built, works, **runs on the operator's laptop** | `src/loadgen/main.go`. There is no manifest for it in `k8s/` |
| **Verifier, executor, contract freeze, state machine** | **DO NOT EXIST** | `src/control/` is absent |
| Reasoning plane | 3 agents built and verified on live evidence | `src/agents/`, `scripts/agent.sh` |
| Scoped Elasticsearch keys | **not created** — everything uses the unrestricted setup key | `elastic/create-api-keys.sh` has not been run successfully |
| AWS credential | **temporary Workshop session** (`ASIA…`, `WSParticipantRole`), has an unmeasured expiry | `aws sts get-caller-identity` |
| Bedrock model access | no Anthropic models. `openai.gpt-oss-120b-1:0` (default), `us.amazon.nova-pro-v1:0`, `us.meta.llama3-3-70b-instruct-v1:0` | `scripts/agent.sh preflight` |
| Load balancer | a **classic ELB**, not the intended NLB | Gateway needs reapplying; see §9 of the architecture doc |

## The critical path

Work these in order. Each is blocked by the one above it.

### 0. Restore cluster access

Nothing that touches Kubernetes works until this is fixed.

```bash
aws eks update-kubeconfig --name sloproof --region us-west-2 \
  --kubeconfig .kube/sloproof.config
KUBECONFIG=.kube/sloproof.config kubectl config current-context
```

Export that context name as `SLOPROOF_KUBE_CONTEXT`, then confirm
`KUBECONFIG=.kube/sloproof.config kubectl get nodes` returns two nodes.

**Read `scripts/_guard.sh` before running any cluster script.** This laptop's
ambient kubectl contexts are unrelated production clusters (`dev_automatic_cluster`,
`genesis-prod-v2`). Every script pins `KUBECONFIG` into the repo and asserts the
context for that reason. Never run an unqualified `kubectl apply` here, and never
weaken the guard to make a command succeed.

Done when: `kubectl get pods -n checkout` lists the checkout workloads.

### 1. Move the load generator into the cluster (decision D9)

**This is the highest-value task and it blocks the showcase scenario.**

The first full run measured healthy v1 at a p95 of **339 ms** against a contract
threshold of **300 ms** — a FAILED verdict for a service that was working
correctly. The service was not slow. The load generator was running in India
against a gateway in `us-west-2`: TCP connect alone cost ~245 ms, while the same
request from inside the cluster completes in ~50 ms. The vantage point was
measuring the operator's geography.

Until the generator runs next to the service, **no run can produce PROVED**, and
the "successful recovery" demo cannot work at all.

Build it:
- A `Job` or `Deployment` manifest at `k8s/50-loadgen.yaml`, namespace `checkout`
- Target the in-cluster gateway Service, not the public load balancer, so the
  measurement path has no internet hop
- Image built and pushed with `scripts/build-image.sh` and `scripts/push-ecr.sh`
  (`src/loadgen/Dockerfile` does not exist yet — write one, mirroring
  `src/checkout/Dockerfile`)
- Elasticsearch credentials from a `Secret`, never baked into the image
- Configuration via the env vars `src/loadgen/main.go` already reads:
  `TARGET_URL`, `RPS`, `DURATION`, `WARMUP`, `RUN_ID`, `TRAFFIC_CLASS`,
  `MAX_INFLIGHT`, `ELASTIC_CLOUD_ENDPOINT`, `ELASTIC_API_KEY`

Do not modify the generator's measurement logic. It is an open-model generator
on purpose: arrivals are scheduled by a ticker and a saturated worker pool counts
a DROPPED arrival rather than delaying it, because a closed-loop generator would
quietly throttle itself as latency rose and an 800 ms regression would appear as
less traffic instead of slow traffic.

Done when: a run with the `healthy` profile on v1 shows a p95 comfortably under
300 ms in `elastic/queries/window-metrics.esql`. Report the measured number. If
it is still above 300 ms, stop and say so — do not adjust the contract to fit
the measurement.

### 2. Build the verifier — `src/control/`

This is the single most important gap. Without it there is no verdict, and the
verdict is the one part of the demo that cannot be faked or narrated around.

Write, in Go to match the rest of the repo:

- **`contract.go`** — load `contracts/checkout-slo.json`, compute its sha256,
  write it to `sloproof-contracts` **before** any routing change. Never edit a
  contract mid-run; a mid-run change invalidates the trial.
- **`verifier.go`** — for each observation window, run
  `elastic/queries/evidence-completeness.esql` first, then
  `window-metrics.esql`. Use a **read-only** key. Emit PROVED, FAILED or
  INCONCLUSIVE per the contract's `verdicts` block, plus the exact ES\|QL text
  and time range it used.
- **`ledger.go`** — append verdicts and receipts to `sloproof-ledger`. The
  mapping is in `elastic/templates/sloproof-ledger.json`.
- **`executor.go`** — patch weights on one named `HTTPRoute`, conditional on
  UID, `resourceVersion`, backend identities and expected current weights. An
  unexpected route change records a conflict and escalates rather than
  overwriting. It may change weights and nothing else — not fault profiles, not
  Deployments, not shell commands.

Three rules that are easy to get wrong and fatal to the claim:

1. **An empty ES\|QL result is missing evidence, never zero errors.** Returning
   PROVED because a query matched nothing is the worst possible failure. The
   contract says so explicitly; enforce it.
2. **Anchor observation windows to data-plane-effective time**, derived from
   observed traffic — not to API acceptance time, and never to planner text. The
   receipt records both timestamps; use the second.
3. **Never use `COUNT_DISTINCT` for duplicate detection.** It is approximate
   (HyperLogLog++) and reads as ~0.8% phantom duplicates at this sample size,
   which would wrongly trigger INCONCLUSIVE. Duplicates are structurally
   impossible anyway: `request_id` is the document `_id`, so a retry overwrites.
   See the comment block in `evidence-completeness.esql`.

Done when: a healthy v1 window returns PROVED and a deliberately slow v1 window
(fault profile `bad-recovery`) returns FAILED, both on real ingested evidence,
with the verdicts in the ledger. Show both.

### 3. Create the scoped API keys

Right now everything — loadgen, verifier, controller, agents — uses one
unrestricted key. The project's central claim is credential separation, so a
judge who asks "what stops the verifier writing the evidence it reads?" currently
has no good answer.

Run `elastic/create-api-keys.sh`. Expect it to refuse: Elasticsearch will not let
an API key mint a narrower child key (a derived key inherits everything or
nothing), so it needs `ELASTIC_USERNAME`/`ELASTIC_PASSWORD` in `.env`, or the
three role descriptors it prints pasted into Kibana → Stack Management → API
keys → User API key. Then run `elastic/verify-key-scopes.sh` to prove the
verifier key actually cannot write.

Point the loadgen manifest at `ELASTIC_INGEST_API_KEY` once it exists.

### 4. Then, and only then

- Fill in `SUBMISSION.md`. Judges read it first and it is currently a template.
- Reapply the Gateway so the load balancer is an NLB rather than the classic ELB
  it fell back to.
- Two clean rehearsals from `scripts/reset.sh`, plus a backup recording.
- `scripts/eks-down.sh` when finished. The cluster bills ~$6.50/day and the
  script releases load balancers *before* deleting the cluster, which is what
  stops an orphaned LB billing on after teardown.

## Constraints

- **No LLM in the decision path.** The three agents in `src/agents/` are
  advisory: an investigator that recommends, a narrator that explains a verdict
  already written, and a briefer that answers questions. `src/agents/ledger.py`
  enforces this — it refuses any event outside `{agent_advisory,
  agent_narrative}` and any payload carrying `verdict`, `contract_hash`,
  `route_uid` or `reason`. Do not relax those guards, and do not give an agent a
  path to the executor.
- **Never commit credentials.** `.env` is gitignored. No script reads or writes
  AWS credentials.
- **The AWS session expires** and nobody has measured how long is left. Check
  before a long task; if `aws sts get-caller-identity` fails, that is the cause.
- **Disclose every fallback and fixture.** The project's credibility rests on
  measured claims. If something is a stub, label it. If a fallback was taken,
  say which. There is already one such disclosure: Agent Builder was verified
  working, but Kibana's `.bedrock` connector cannot hold a session token, so the
  agents call Bedrock directly.
- **Do not adjust the contract to make a verdict come out well.** If healthy v1
  fails the threshold, that is a finding, and the last time it happened it was
  the measurement vantage point that was wrong, not the service.

## Report back

State what you changed, what you measured, and what you could not verify. Quote
real numbers with the query that produced them. If a step is blocked, finish
everything else and say explicitly what you left undone and why.
