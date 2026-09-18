# The three rollout outcomes

The Go verifier still issues only PROVED, FAILED or INCONCLUSIVE. A separate
deterministic rollout policy turns that evidence into an operational next step.
An operator explicitly starts the rollout plan; no LLM can initiate or authorize
a traffic change. `sloproof-loop` branches on the recorded policy and sends a
Slack alert on each of the three outcomes before running advisory agents. The
passing alert states explicitly that it is not an authorization.

| Case | Action | Guard |
|---|---|---|
| Candidate meets the frozen metrics | Continue the fixed **10 → 25 → 50 → 100%** plan, and alert Slack | Each adjacent stage needs a fresh PROVED result, unchanged candidate configuration and route, then a new frozen trial and observation windows. |
| Candidate and original both breach | **Pause expansion and alert Slack** | Compare matching windows; investigate common dependencies, infrastructure and configuration. A common cause is a hypothesis, not a diagnosis. Do not automatically return traffic to another unhealthy version. |
| Evidence is inconclusive | **Hold traffic and resolve uncertainty** | Retry explicit temporary evidence delays only; repair invalid evidence rather than repeatedly executing the intervention. |

## Smart handling of inconclusive evidence

`scripts/verify-with-retry.py` calls the isolated read-only Go verifier. It keeps
the run ID, frozen criteria, observed-route anchor and window boundaries fixed.
It never repeats a route patch or moves windows forward to search for a pass.

- Temporary ES 429/502/503/504 errors, partial query responses, unfinished
  ingestion allowance, or indexed records not yet visible may be rechecked.
- At most **two additional attempts**, **30 seconds apart**, within a **120-second
  verification budget**. A process timeout stops progression; it is not a pass.
- A missing receipt, route/configuration mismatch, malformed evidence, dropped
  arrivals, or an underpowered completed cohort requires repair and a new trial.
  Waiting cannot add requests to a completed, fully reconciled cohort.
- A window whose observed candidate share diverges from the executed route
  receipt by more than +/-0.05 is INCONCLUSIVE, not FAILED or PROVED. A trial
  whose routing drifted to full candidate traffic did not measure the bounded
  stage that was frozen, so neither outcome may be claimed for it.
- Any conclusive result ends retries immediately. Retain every actual attempt;
  append only the final verifier result as the unique canonical decision.
- Exhausted retries become HOLD_AND_ESCALATE. A workflow replay cannot reset
  the budget. Inconclusive results never authorize expansion or blind rollback.

## Run the plan

After restoring AWS authentication and exporting the scoped credentials, pinned
context and load-generator image from [the runbook](DEMO_RUNBOOK.md):

```bash
scripts/rollout.sh healthy demo-new-healthy
# Or the deliberately inadequate candidate, which stops after its failed stage:
scripts/rollout.sh bad-recovery demo-new-bad
```

Use fresh prefixes. The healthy plan runs four separate five-minute generated
traffic stages, approximately 20 minutes plus setup/verification. Every stage
has its own contract, receipt, summary and verdict. The first scenario setup
establishes the injected incident; subsequent stages preserve the candidate
configuration and existing routing until the guarded adjacent patch.

The native Elastic workflow runs after every stage. Slack notifications default
on for this explicit rollout. Set `SLOPROOF_SLACK=0` for a silent rehearsal, or
`SLOPROOF_ELASTIC_REVIEW=0` to omit the advisory/notification integration entirely.
These switches do not relax the verifier or progression guards.

To continue one recent proved stage without resetting the incident:

```bash
scripts/run-trial.sh healthy new-stage-id --from-run previous-proved-stage
```

The controller derives the next weight; callers cannot request a skipped stage.
It refuses a verdict or final observation older than 15 minutes, a changed
candidate hash, a changed route UID/resourceVersion/weights, or a reused run ID.
Saved morning demo passes therefore cannot authorize later live traffic changes.

## Slack and the native workflow

`elastic/workflows/sloproof-loop.yaml` now performs:

1. Read exactly one deterministic verdict.
2. Read exactly one deterministic rollout policy with the same contract and
   source verdict ID.
3. Select continued rollout, paused failed recovery, or evidence hold.
4. Send the fixed operational alert to `#all-on-call-engineering-team` when
   notifications are enabled. No agent prose controls this message or branch.
5. Run Agent Builder investigation/explanation afterward. Agent failure cannot
   prevent an earlier alert; notification errors remain workflow failures.

The workflow does not execute Kubernetes commands. The explicitly started
rollout runner and conditional Go executor own stage execution. Native workflow
completion is not proof that a subsequent traffic stage has run.

To replay a recorded case:

```bash
.venv/bin/python elastic/manage.py run demo-slow-20260918-02 --attempt 3 --no-notify
# Remove --no-notify only when another real Slack alert is intended.
.venv/bin/python elastic/manage.py collect demo-slow-20260918-02 --attempt 3
```

Replay sends a new alert when notifications are enabled; it is not a deduplicated
paging system. Slack step retries are not configured. The workflow has read-only
agent tools and the specific Slack connector, not arbitrary HTTP execution.
The Go controller records policy separately from the original verifier document.

## What has actually been exercised

Native Elastic executions on retained EKS trials completed:

| Original result | Execution | Observed operational branch |
|---|---|---|
| FAILED, both cohorts breach | `c92b3b29-0470-4121-b2b2-c20d07856658` | `pause_rollout`, then `notify_failed_rollout`; Slack returned `ok: true`, timestamp `1789724091.666839`. |
| PROVED | `26d7ada9-dc4b-4e25-845d-57ca2dce83c7` | `continue_planned_rollout`; no failure alert. |
| INCONCLUSIVE, missing receipt | `c5c2799d-c23d-4903-aa05-e780efa0484a` | `hold_rollout`, metric investigation skipped; silent replay, no Slack message. |

Artifacts are `evidence/*-attempt-2-elastic-review.json` and
`evidence/*-rollout-policy.json`. Original verdicts and prior reviews are retained.
The exact HTTP 403 credential checks remain available separately.

Guarded progression and retry classifications have local regression tests.
**Fresh 25/50/100% EKS stages have not been live-validated: the AWS session
expired.** The demonstrated PROVED evidence remains the earlier 10% candidate
trial. The transient-retry path has local checks; the retained live INCONCLUSIVE
case is a missing receipt, not a telemetry-loss or successful retry demo.

This remains a prototype with generated traffic, injected faults and no durable
restart recovery, independent expiry watchdog, or automatic reversal. A proved
100% stage would establish compliance only at its measured generated load.

API reference: [Elastic Slack connector](https://www.elastic.co/docs/reference/kibana/connectors-kibana/slack-action-type).
