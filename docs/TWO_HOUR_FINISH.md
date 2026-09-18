# Two-hour finish plan

Progress: scoped keys verified and live trials exercised. Use
[live validation](LIVE_VALIDATION_20260918.md) for measured outcomes, not the
historical fallback plan below. Final presentation, team names, recording and
external submission remain operator tasks.

Scope: demonstrate operator-supervised recovery verification using existing EKS,
Elasticsearch, and Bedrock components. The current runner leaves routing unchanged
after a verdict. Argo integration and automatic promotion are future work.

| Elapsed | Deliverable | Completion evidence |
|---|---|---|
| 0–15 min | Supply the three scoped Elastic keys locally; verify scopes and AWS session | Expected allowed/forbidden results from `elastic/verify-key-scopes.sh` |
| 15–40 min | Run slow and healthy candidate scenarios sequentially using fresh run IDs | Real FAILED and PROVED results, frozen contracts, receipts, completed Job logs |
| 40–55 min | Exercise insufficient evidence; obtain AI explanation of actual trial evidence | INCONCLUSIVE reason; cited advisory and narrative; no fixture verdicts |
| 55–75 min | Regenerate the evidence export and finalize submission | Saved real results visible; measured claims agree with artifacts |
| 75–90 min | First rehearsal and fix only demonstration blockers | Four-minute walkthrough without missing files or unsupported claims |
| 90–120 min | Feature freeze, second rehearsal, backup capture and submission preparation | Portable evidence bundle; recording if available; names and delivery link supplied |

## Credential gate

Add these to the ignored repository `.env`, never to chat or tracked files:

- `ELASTIC_INGEST_API_KEY`
- `ELASTIC_VERIFIER_API_KEY`
- `ELASTIC_CONTROLLER_API_KEY`

The existing key-creation script contains the role descriptors. It can provision
keys if authorized user credentials are provided locally as `ELASTIC_USERNAME`
and `ELASTIC_PASSWORD`. The existing setup API key cannot create these narrower
keys. Keep the verifier's privilege check intact.

## If the credential gate is still closed after 15 minutes

Prepare the strongest demonstrable subset: measured 57.48 ms healthy latency,
reconciled 8,000-record run, a live evidence-grounded agent answer, and the real
unrestricted-credential refusal. Disclose that recovery verdict validation is
blocked. Do not relabel the latency check as PROVED or populate the export with
unit-test fixtures. Retry the full scenarios if credentials arrive in time.

## Scenario procedure

Use `docs/DEMO_RUNBOOK.md` for the pinned context, image digest, and exact trial
commands. Keep the scopes check ahead of any mutations. Review actual verdict
reasons: exit code 2 can mean FAILED or INCONCLUSIVE and is not by itself evidence
of a successful negative scenario.

Obtain a Narrator explanation only after a real verdict has been recorded. Before
demonstrating, refresh the static export with `python3 scripts/render-evidence.py`.

Team-supplied submission details: member names and the final recording/delivery
link. The local submission draft must be reviewed before external submission.
