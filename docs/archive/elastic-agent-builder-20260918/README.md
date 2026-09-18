# Archived Elastic Agent Builder prototype

**Active integration:** corrected agents, tools and a review workflow are now
maintained under [`elastic/`](../../../elastic/README.md). This directory keeps
the original export history, rather than the active definitions.

These are source snapshots, not the canonical demo configuration. They preserve
the export added by commit `654cf895d90c823c3e931a9d2545831eb93de215`, reviewed
at feature-branch head `87fad88dc3d8a53ffa18667c8febf8996a9e5a43`. The original
14 files are unchanged; the source README is named `ORIGINAL_README.md` here.

Do not apply these definitions to the demo. The canonical workflow is
[`scripts/run-trial.sh`](../../../scripts/run-trial.sh), using the deterministic
[`src/control/`](../../../src/control/README.md) verifier and the scoped
credentials described in [live validation](../../LIVE_VALIDATION_20260918.md).
Active Elasticsearch mappings and queries remain under `elastic/templates/` and
`elastic/queries/` at the repository root.

The prototype differs materially from the measured demo:

- Its `sloproof_verifier` is an LLM agent. Its output controls rollback and the
  ledger's verdict; our demo requires deterministic verification.
- Its workflow is exported with `enabled: true` even though its alerting rule
  is disabled. Archiving the files does not disable anything already deployed.
- It anchors queries to API acceptance time, has no bounded end timestamp, and
  does not enforce the three closed observation windows or request-record
  reconciliation used by the canonical verifier.
- The latency tool measures p99, while the frozen demo contract specifies p95.
  The dependency tool counts only error spans, which cannot distinguish absent
  telemetry from a healthy dependency. The quote span is an in-process simulation.
- Investigator plans name `quote-v1` and `quote-v2`; the actual route uses
  checkout backends. Execution depends on the deferred Lambda implementation.
- The export README's credential, model-access and deployment statements are
  historical author claims. They are not current runtime validation. Its index
  definitions differ from the canonical mappings and must not replace them.

Connector secret headers were redacted in the source export. No workflow,
connector, alert, Lambda or cluster setting was changed during consolidation.
The original Lambda source remains accessible in main's Git history at
`bdb8dbd:infra/lambda/executor/`; its exclusion from the active demo is explained
in [the branch review](../../BRANCH_REVIEW.md).
