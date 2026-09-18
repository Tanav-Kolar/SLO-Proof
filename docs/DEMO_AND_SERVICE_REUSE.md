# SLOProof demo and service reuse

Status: proposed implementation and presentation plan, 18 September 2026. This document describes intended behavior, not completed capabilities.

## Recommendation

Build the first demo around a small simulated checkout API. Show a bad release breaking checkout, a recovery tested on 10% of traffic, and promotion only after predefined checks pass against evidence in Elasticsearch.

The distinguishing feature is the recovery decision: an agent can recommend an action, but a deterministic verifier decides whether the evidence satisfies the acceptance criteria. Demonstrate both a successful recovery and a rejected recovery.

Initial product scope: evidence-gated recovery for stateless HTTP services with controllable traffic routing. The initial customer is a platform or SRE team operating Kubernetes services and using Elastic.

## Demo service

Expose `POST /checkout` and a separate health endpoint. Simulate the transaction without real payments, persistent business data, or downstream services in the first implementation.

| Deployment | Behavior | Purpose |
|---|---|---|
| `checkout-v1` | Successful responses with latency comfortably below 300 ms at the target load | Known-good recovery candidate |
| `checkout-v2` | Approximately 20% HTTP 500 responses and an added 800 ms delay | Repeatable application regression |
| `checkout-v1` with a bad-recovery profile | Successful responses but latency above 300 ms | Prove that lower errors alone cannot authorize promotion |

Return the serving version and request ID on every application response, including errors. Configure faults through operator-controlled deployment settings. Keep health probes healthy during the application regression so Kubernetes does not remove the failing version before the experiment.

Generate approximately 100 requests per second through one shared route. At a 10% trial split, this should yield about 600 trial requests per minute; decisions must use measured counts. Record achieved traffic and dropped load-generator arrivals.

## Architecture and responsibilities

```text
Load generator -> weighted route -> checkout v1 / v2
       |
       +-> client-observed outcomes -> Elasticsearch
                                           |
                          +----------------+----------------+
                          |                                 |
                   Agent investigation              Deterministic verifier
                   and recommendation               against saved criteria
                          |                                 |
                          +-> bounded controller <----------+
                                      |
                                route changes
                                      |
                           evidence and verdict ledger
```

- **Service and routing:** healthy/faulty deployments, actual weighted traffic, fault controls, and reset commands.
- **Elastic:** request observations, parameterized ES|QL investigation tools, saved criteria, verifier queries, and an inspectable evidence ledger.
- **Agent:** investigate the incident, cite measurements, and recommend an allowed recovery. It cannot rewrite criteria to obtain a pass.
- **Verifier:** calculate measurements from Elasticsearch and return `PROVED`, `FAILED`, or `INCONCLUSIVE` with reasons.
- **Controller:** apply the authorized trial, conditional promotion, or reversal and record routing receipts.

The hosting scope is **basic EKS** (`eksctl`, one managed node group), with Elastic Agent Builder and a Bedrock integration if available. This was revised on 18 September 2026 once the AWS credential in use was confirmed to hold AdministratorAccess; local Kubernetes remains the fallback if provisioning stalls. Confirm the event's required services separately; the judging slide alone does not establish that EKS, Bedrock, SageMaker, or all of them are mandatory. Disclose any fixture or manual step used in place of an integration.

## Metrics

Show customer-facing metrics overall and separately for v1 and v2. During a 10% recovery trial, overall health will remain poor because most traffic still reaches the faulty version.

| Metric | Definition or measurement | Role in the demo |
|---|---|---|
| Checkout success rate | Valid successful checkout responses divided by eligible attempted requests; timeouts and network failures count as failures | Trial target: at least 99% |
| p95 latency | Client-observed elapsed time for eligible attempts, including failures; record timeout elapsed time and freeze the percentile method | Trial target: below 300 ms |
| Request rate and count | Achieved requests per second and eligible requests per closed window | At least 400 trial requests per 60-second window |
| Observed traffic split | Actual version-attributed request counts, with unknown-version outcomes reported separately | Show approximately 10% v1 / 90% v2 during the trial |
| Evidence completeness and delay | Reconcile observed requests with searchable records; measure indexing delay | Missing or late evidence blocks promotion |
| Recovery duration | Incident start to completion of successful full-traffic verification | Measured operational outcome for the run |
| Failed checkout count | Count of failed attempts before, during, and after recovery | Visible customer impact under generated load |
| CPU, memory, and pod restarts | Resource usage and workload health by deployment | Supporting diagnostics if time permits |

Record request ID, run ID, request start/end time, duration, HTTP outcome or timeout, serving version when known, and warmup/synthetic flags. Keep request IDs out of metric dimensions. Use stable document IDs to avoid duplicate evidence on ingestion retries, and keep indexing outside request timing.

Latency, traffic, errors, and saturation follow the monitoring foundation described in [Google's SRE guidance](https://sre.google/sre-book/monitoring-distributed-systems/). The numerical targets here are proposed demo acceptance criteria, not universal production SLOs.

## Recovery contract and verdict

Save the criteria and their hash in Elasticsearch before the trial begins. Include the service and route identity, original/trial/promotion weights, eligible request population, thresholds, windows, timeout treatment, percentile method, and ingestion deadline.

1. Confirm that the trial routing has taken effect using observed traffic. Record both route-update acceptance and observed effect times.
2. Evaluate three consecutive, non-overlapping 60-second windows after routing takes effect; exclude warmup and separately labeled synthetic traffic.
3. In every window, require at least 400 v1 requests, success rate at least 99%, and p95 latency below 300 ms.
4. Reconcile each window's observations with searchable Elasticsearch records. Proposed initial ingestion allowance: 30 seconds after window close; validate this in rehearsal and freeze it before the run.
5. Query Elasticsearch for the decision. Save exact ES|QL queries, time ranges, sample counts, results, and verdict reasons in the ledger.

| Verdict | Meaning | Controller action |
|---|---|---|
| `PROVED` | All three windows meet all criteria with complete evidence | Route 100% to v1, then verify at full load |
| `FAILED` | Reliable measurements establish a threshold breach | Restore pre-trial weights and keep the incident open |
| `INCONCLUSIVE` | Evidence is missing, late, insufficient, or cannot be attributed reliably | Do not promote; restore pre-trial weights and keep the incident open |

Gateway failures and unknown-version requests must remain visible. If attribution gaps could change the trial's result, the verifier cannot issue `PROVED`. Empty queries do not mean zero errors. Local calculations cannot replace missing Elasticsearch evidence.

After promotion, measure at least one complete 60-second window at full traffic against the same success and latency criteria, with complete evidence and at least 400 requests. Record trial success separately from verified service recovery. If full-load checks fail or evidence is incomplete, keep the incident open and escalate to the operator; do not assume returning traffic to faulty v2 is a safe recovery.

These checks demonstrate acceptance at the tested load and duration; they do not establish future production reliability. A contract hash provides traceability, not privilege-enforced immutability.

## Presentation sequence

1. **Healthy baseline:** send 100% of traffic to v1 and show healthy checkout measurements.
2. **Incident:** the operator sends traffic to faulty v2; errors and latency rise. Label the trigger as manual unless automated detection exists.
3. **Investigation:** the agent queries Elastic, identifies the version-specific regression, and recommends a limited trial of v1 with evidence links.
4. **Commit and trial:** display the saved criteria and start the authorized 90% v2 / 10% v1 trial. Explain that promotion is conditional on the verifier.
5. **Proof and recovery:** display the three windows, verdict, promotion to v1, and subsequent full-load verification.
6. **Rejected recovery:** run the slow-v1 scenario. Show that successful responses alone are insufficient: latency fails, promotion is blocked, and the trial is reversed.

Allow approximately 8–12 minutes for the main scenario, depending on investigation, routing, and ingestion delays. The three observation windows alone require three minutes. Use saved evidence and a clearly labeled recording for the rejected scenario if presentation time is limited.

Reversal of a rejected trial restores the incident route, which is still degraded. It rejects the proposed repair; it does not resolve the incident. Reset faults and routing separately between demonstrations.

## Dashboard and business impact

Use one incident view with:

- Overall and per-version success rate and p95 latency.
- Actual traffic split and per-window request counts.
- Evidence completeness, saved criteria, and verdict reasons.
- A timeline of incident start, trial, routing effect, verdict, promotion/reversal, and recovery verification.
- Failed checkouts and measured recovery duration for the run.

Show generated traffic as generated traffic. To claim failures avoided, compare against a no-recovery run with the same load, fault profile, and duration. Without that comparison, report observed failures and recovery duration only. Label any revenue estimate as modeled and state its assumptions.

## Reuse across services and APIs

Reuse the process: propose an allowed action, run a bounded trial, evaluate saved criteria, then promote or reverse. Each service needs meaningful correctness checks as well as operational metrics.

| Service | What recovery should prove | Possible bounded action | Additional concern |
|---|---|---|---|
| Checkout API | Successful checkout and acceptable latency | Shift traffic to a healthy version | Real transactions need idempotency and side-effect handling |
| Search API | Latency, errors, and deterministic result-quality checks | Trial an earlier search backend | HTTP 200 alone does not establish useful results |
| Authentication API | Valid logins succeed and invalid credentials remain rejected | Trial a known-good deployment | Separate expected rejections from service failures |
| AI inference API | Latency, output quality, and cost per request | Trial an alternative model endpoint | Requires task-specific evaluation and compatible data handling |
| Background worker | Processing success, queue age, and duplicate handling | Trial a worker pool on selected jobs | Requires queue assignment and job-completion evidence instead of HTTP routing |

The first two fit the initial HTTP demo closely. Authentication needs security-specific checks. AI inference and workers illustrate later extensions, not capabilities established by the checkout demo. Reversing routing cannot undo payments, writes, or already processed jobs.

Make these inputs configurable: service identity, version identities, route, allowed action, trial percentage, metric queries, correctness checks, thresholds, sample minimums, observation windows, ingestion allowance, and post-promotion behavior. Query templates and routing actions should use validated parameters and scoped credentials.

After checkout is stable, add a small search API using the same controller, verifier, and ledger. Give it a fixed query fixture whose expected result is known. Demonstrate reuse by changing service configuration and supplying its correctness check. If new orchestration code is needed, disclose that adaptation rather than claiming configuration-only reuse.

## Build priorities and completion criteria

The confirmed judging weights are production readiness 30%, platform depth 25%, measurable business impact 25%, innovation 10%, and go-to-market reusability 10%. The following priorities are team recommendations for demonstrating those categories.

| Priority | Deliverable | Completion evidence |
|---|---|---|
| 1 | Repeatable checkout incident and real evidence path | Deploy/load/reset commands; actual observations indexed and queried in Elastic |
| 2 | Successful and rejected recovery | Saved criteria, routing receipts, three passing windows, full-load check, and a rejected slow trial |
| 3 | Evidence-grounded agent investigation | Inspectable tool queries and measurements supporting the recommendation |
| 4 | Clear dashboard and business outcomes | Per-version metrics, verdict reasons, observed failures, and recovery duration |
| 5 | Reuse demonstration | Second API using shared orchestration and service-specific configuration |

Before presentation, complete two clean rehearsals, verify reset behavior, and save a backup recording plus evidence exports. A missing-evidence scenario is a useful additional demonstration after the successful and rejected trials work.

Keep operational limits explicit: an operator-supervised controller is not an independent expiry watchdog; a manual incident trigger is not automated detection; and a trial that passes at 10% is not evidence that v1 sustains full traffic. Protect rehearsal time before adding infrastructure or more services.

Judge-facing message: **“The agent proposes a recovery. Measured evidence decides whether it can expand.”**
