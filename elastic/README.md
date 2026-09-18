# Elastic-side definitions

Everything under this directory is an **exported snapshot** of what's live in
Elastic Cloud, not something applied from here. It was created via direct
`elastic` CLI calls against Kibana/Elasticsearch (see
`steering_docs/sloproof-steering-doc` for the design); these files exist so
the repo has a reviewable, version-controlled record of that state, and so a
fresh setup can be reproduced without re-deriving every field by hand.

To re-apply after a change, feed each file back through the matching
`elastic` command, e.g.:

```
elastic es indices create --input-file elastic/indices/sloproof-contracts.json
elastic kb agent-builder post-agent-builder-tools --input-file elastic/tools/get_checkout_incident.json
elastic kb agent-builder post-agent-builder-agents --input-file elastic/agents/sloproof_investigator.json
elastic kb workflows post-workflows-workflow --id sloproof-loop --yaml "$(cat elastic/workflows/sloproof-loop.yaml)"
elastic kb alerting post-alerting-rule-id --input-file elastic/rules/sloproof-burn-detection.json
```

## Layout

- `indices/` — mappings for the 3 system indices (`sloproof-contracts`,
  `sloproof-evidence`, `sloproof-ledger`). Two leftover index *templates*
  from an earlier draft (same names, different fields) were found and
  deleted during this build — see the steering doc's "API keys" section for
  the related note on what else didn't carry over from that draft.
- `tools/` — the 4 Agent Builder ES|QL tools. Queries target
  `traces-*.otel-*` (the hack-day/real-OTel form from the steering doc, not
  the practice/simulator form), since `src/checkout` now exports real OTel
  traces.
- `agents/` — `sloproof_investigator` and `sloproof_verifier`. Model is
  Elastic's default hosted inference (not Bedrock — tried, blocked on AWS
  Marketplace access in this account; team decided to stay on the default).
- `workflows/sloproof-loop.yaml` — the 8-step orchestration. Note: object-
  typed template values (`{{ ... }}`) render as JS `[object Object]` when
  targeting an ES `object`-mapped field in this workflow engine — static
  values are hardcoded instead of templated, and the one genuinely dynamic
  object (`execution_receipt`, sourced from the Lambda) is stored as a JSON
  string with the ledger field mapped `text`, not `object`.
- `rules/sloproof-burn-detection.json` — created **disabled**; enable once
  you're ready to let real traffic drive the loop.
- `connectors/` — `checkout-remediation-api` (points at the deployed Lambda's
  Function URL) and `sloproof-slack-alerts` (still a placeholder — no real
  Slack workspace wired up yet). Secret header values are redacted in these
  files; the real values live only in Kibana and in the Lambda's own
  environment (`EXECUTOR_SHARED_SECRET`).

## Known gaps (as of this export)

- The Lambda executor's public Function URL currently returns 403 on an
  unauthenticated request; only proven via direct SDK invoke so far
  (`aws lambda invoke`), which bypasses that layer. The real Kibana →
  connector → Lambda path isn't confirmed working end-to-end yet.
- `sloproof-slack-alerts` has no real webhook/token behind it.
- Three additional API keys the steering doc originally specified
  (`sloproof-investigator`, `sloproof-orchestrator`, `sloproof-verifier`)
  were designed, created, and then invalidated once it was confirmed that
  Agent Builder agents and Workflow steps have no field to attach a custom
  per-component credential — see the steering doc for the full note.
