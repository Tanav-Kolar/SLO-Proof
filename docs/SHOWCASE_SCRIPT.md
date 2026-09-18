# Would you ship this recovery?

The three operational outcomes and latest Slack receipt are documented in [ROLLOUT_CASES.md](ROLLOUT_CASES.md). Use **Next step / Slack** after a reveal to show continued rollout, a paused failed recovery, or the bounded response to insufficient evidence.

Use a four-minute audience decision challenge. Open `demo/index.html` or
`http://127.0.0.1:8768/` and reset the presentation before starting. Show this
screen first; keep the full evidence view ready for questions.

The opening sentence is: **“Our candidate returns 100% successful checkouts.
Would you ship it?”** Let one judge make a call. The surprise is that a real
improvement can still fail the contract, and a failed candidate does not imply
returning traffic to the worse version.

## Stage sequence

| Time | Action | Say |
|---|---|---|
| 0:00–0:20 | Candidate A. Point to 100% success and the frozen criteria. Ask for a show of hands; select the audience prediction. | “Every candidate checkout succeeded. Does this recovery meet the whole contract?” |
| 0:20–1:10 | Reveal **FAILED**. Point to all three p95 windows. | “About 508 ms still misses our strict 300 ms target. Better is not yet recovered. FAILED does not send users back to the worse incident version.” |
| 1:10–1:55 | Open **Why?** Show completed Elastic steps and the saved Evidence Briefer response. | “Elastic investigates the measured window and explains the Go decision. Its agents have no routing or verdict-writing tool. The deterministic policy branch and configured Slack alert happen before advisory AI.” |
| 1:55–2:35 | Close the dialog, select **Next case**, reveal **PROVED**. | “Same contract. About 57 ms, 100% success, 600 candidate requests in each window. This passing stage permits the next step of an operator-started rollout plan. Every expanded stage needs a new measurement; the later stages are not part of this saved trial.” |
| 2:35–3:05 | Next case, reveal **INCONCLUSIVE**. | “This execution was refused and has no observed-route receipt. No measurements cannot become zero errors. The verifier declines to establish recovery.” |
| 3:05–3:40 | **Show the trust boundary**. Show the actual 403 results and zero-agent-call check. | “The verifier cannot rewrite requests, contracts, or the ledger. The Elastic workflow also refuses to call an agent without a recorded decision.” |
| 3:40–4:00 | Stay on the boundary view; close with the product. | “For platform teams, SLOProof turns a proposed recovery into an inspectable decision: frozen criteria, measured outcomes, and a receipt. AI can make a case for recovery. It cannot award itself a pass.” |

The saved trials used generated traffic and injected configuration faults on
EKS. The subsequent native Elastic Workflow executions are also recorded. Say
this once at the start; the screen keeps the disclosure visible. The reveal
buttons reveal retained results; they neither rerun verification nor move traffic.

## Why this is the strongest demonstration

The audience makes a concrete decision before seeing the answer. Then the
product demonstrates three different outcomes under one contract, with exact
queries behind every measured window. The actual credential denials turn the
separation claim into something a judge can inspect. Elastic contributes real
investigation and explanation, while the acceptance decision stays independent.

Spend stage time on that sequence. Keep deploy commands, raw JSON, branch
history, and secondary integrations for questions. All first-window comparisons
use contemporaneous v1/v2 cohorts. No run-wide p95 is computed by averaging window
percentiles. Do not claim revenue saved, failed requests avoided, or reduced MTTR.

## Questions to be ready for

- **“Why not roll back a FAILED candidate?”** It improved on v2 but missed the
  absolute target. Every verdict leaves the incident OPEN and routing unchanged;
  an operator chooses the next mitigation. The verifier does not rank remedies.
- **“Is PROVED permission to send 100% traffic?”** It means the observed trial
  met its frozen contract. Expansion needs operator review and separate evidence
  at the new load.
- **“Did you test telemetry loss?”** The displayed INCONCLUSIVE run is a real
  missing receipt after refused execution. A telemetry-loss scenario has not
  been demonstrated with a retained live trial.
- **“What does the AI actually do?”** The native workflow reads a Go verdict,
  checks uniqueness, invokes Agent Builder for bounded investigation, then
  explains the existing verdict. The Briefer answers evidence questions. AI
  text can be wrong; earlier erroneous answers are preserved for inspection.
- **“Where is AWS?”** EKS hosts checkout, Envoy and the in-cluster load generator;
  ECR stores images. A separate advisory agent path uses Bedrock. Native Agent
  Builder uses Elastic’s configured model connector, not that Bedrock path.
- **“Are all components equally scoped?”** The Go ingest/verifier/controller
  boundaries were checked live. Native Elastic review uses read-only tools under
  the setup/workflow identity; that is a different credential boundary.
- **“Who would use this?”** Platform/SRE teams evaluating recovery trials. A next
  product integration is an existing rollout controller with these review gates.
  General rollout integration and durable restart/expiry handling remain future work.

## Rehearsal and fallback

Use `R` to reveal, arrow keys to change cases, `A` for Elastic, `P` for proof,
and `?` for presenter cues. `Esc` closes dialogs. Reset returns to the opening.
Run the whole sequence twice in the actual presentation viewport; then record
one clean pass before changing anything else.

The portable bundle contains `demo/index.html`, `demo/evidence.html`, retained
JSON evidence, and this script. Extract it and open `demo/index.html` directly if
the server or network is unavailable. Keep live cloud reruns out of the critical
four minutes. The five-minute observation period has already been recorded in
the retained measurements; never simulate it as live progress.

If a judge requests a fresh Elastic answer and credentials are still valid, the
optional command is `.venv/bin/python elastic/manage.py ask <run-id> '<question>'`
with a new explicit attempt number. It is a real advisory model call with
variable latency; it does not start a trial. The saved walkthrough is complete
without it.
