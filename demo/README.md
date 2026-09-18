# Present “Would you ship this recovery?”

Open `index.html` in a browser, or serve this directory locally:

```bash
python3 -m http.server 8768 --bind 127.0.0.1 --directory demo
```

From the repository or extracted bundle root, open
`http://127.0.0.1:8768/`. This page embeds its evidence and needs no
cloud credentials. It shows saved EKS trial results and later native Elastic
Workflow reviews; it is not a live monitor.

After revealing a result, **Next step / Slack** shows its deterministic rollout
policy and the actual notification receipt. The passing case permits the next
stage of a 10/25/50/100% plan; failed recovery pauses and alerts; inconclusive
evidence is held for bounded rechecking or repair. See
[the three cases and validation limits](../docs/ROLLOUT_CASES.md). The retained
trials do not include live 25/50/100% executions.

Open `process.html` for a stage-by-stage product walkthrough: frozen contract,
bounded trial, observation windows, deterministic verification, advisory AI
explanation, and operator review. It uses the same embedded evidence and keeps
queries, receipts, logs, and raw JSON behind expandable details.

Ask the audience to predict the verdict. Candidate A has 100% success but
~508 ms p95: reveal FAILED, then open the saved Elastic explanation. Candidate B
meets the same frozen contract at ~57 ms: reveal PROVED for the observed trial.
Finish with the actual missing-receipt INCONCLUSIVE result and “Prove the
separation”: real HTTP 403 write refusals and a separate workflow check that
calls no agent without a recorded decision.

The audience choice and reveal buttons affect the presentation only. They never
issue a verdict or route traffic. All measurements and decisions come from the
same retained artifacts used by `evidence.html`, which remains the full audit
view. No model or cloud connection is needed to present the saved walkthrough.

Use **Presenter cues** for the four-minute script, or read
[`docs/SHOWCASE_SCRIPT.md`](../docs/SHOWCASE_SCRIPT.md). Keyboard controls: `R`
reveals, `→` advances, `1`/`2`/`3` selects a case, `A` opens Elastic, `P` opens
proof, `?` opens cues, and `Esc` closes a dialog. **Reset presentation** clears
audience calls and reveals. Rehearse once in the actual projector viewport.

Expand queries, workflow steps, or agent output when asked. The source artifacts
retain earlier generated answers with known errors. AI text remains advisory.
Use the full repository and `elastic/README.md` for live workflow commands;
the portable ZIP is an evidence bundle, not a complete deployment package.

To rebuild both HTML pages from source, run `python3 scripts/render-evidence.py`
from the full repository root. The generated pages also work directly from disk
after extracting the portable bundle.
