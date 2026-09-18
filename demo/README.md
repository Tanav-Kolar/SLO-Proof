# Present the saved demo

Open `evidence.html` in a browser, or serve this directory locally:

```bash
python3 -m http.server 8768 --bind 127.0.0.1 --directory demo
```

From the repository or extracted bundle root, open
`http://127.0.0.1:8768/evidence.html`. This page embeds its evidence and needs no
cloud credentials. It shows saved EKS trial results and later native Elastic
Workflow reviews; it is not a live monitor.

Present the FAILED candidate first, then the PROVED candidate, then the actual
missing-receipt INCONCLUSIVE result. Follow with the Elastic workflow and its
Evidence Briefer answer. The separate missing-decision check demonstrates that
the workflow calls no agent when there is no recorded verdict.

Expand queries, workflow steps, or agent output when asked. The source artifacts
retain earlier generated answers with known errors. AI text remains advisory.
Use the full repository and `elastic/README.md` for live workflow commands;
the portable ZIP is an evidence bundle, not a complete deployment package.
