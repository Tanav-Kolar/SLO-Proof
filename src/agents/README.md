# Reasoning plane — three advisory agents

This directory contains the direct Bedrock path. An additional, live Elastic
Agent Builder and Workflows integration now lives under
[`elastic/`](../../elastic/README.md). It uses Elastic's configured model connector
to investigate bounded evidence and explain an existing Go verdict. The earlier
connector limitation below describes Bedrock through Kibana, not the availability
of Agent Builder itself.

Three agents, none of them in the decision path:

| Agent | Command | Reads | Writes | Refuses |
|---|---|---|---|---|
| **Investigator** | `investigate` | `evidence-completeness`, `window-metrics`, `traffic-split` for one window | `agent_advisory` | any action but `inconclusive` when a version is under 400 samples (recorded as a problem, not silently corrected) |
| **Narrator** | `narrate` | `incident-timeline`, `outcome-breakdown`, plus the verdict | `agent_narrative` | to run at all when no verdict exists yet |
| **Briefer** | `ask` | whichever of the five queries the question needs, chosen by the model | nothing | to answer without citing a query it actually ran |

```bash
scripts/agent.sh preflight
scripts/agent.sh investigate --run-id demo-1 \
    --window-start 2026-09-18T06:28:42Z --window-end 2026-09-18T06:31:12Z
scripts/agent.sh narrate --run-id demo-1
scripts/agent.sh ask --run-id demo-1 "what share of traffic did v1 serve during the trial?"
```

`scripts/agent.sh` builds `.venv` on first use (~15 s) and sources `.env`. Add
`--no-write` to `investigate` or `narrate` to skip the ledger.

## Why this is not Agent Builder

Agent Builder is not broken. It was verified live on 18 September 2026 —
`POST /api/agent_builder/tools` created a parameterized ES|QL tool, `_execute`
returned real rows, `POST /api/agent_builder/agents` created an agent, and
`converse` answered correctly with a tool call in 6.3 s. Anyone who wants the
managed path can have it.

What does not work is **Kibana's `.bedrock` connector against this account**.
Its credential form has an access key and a secret and no session-token field,
and the credentials in `.env` are temporary STS credentials (`ASIA…`,
`assumed-role/WSParticipantRole/Participant`). A temporary access key without
its session token is not a valid credential, so the connector returns:

```
Status code: 403. API Error: Forbidden - The security token included in the request is invalid.
```

Calling Bedrock directly passes the session token, which is the entire
difference. This is the `Investigator` fallback from
[SERVICE_ARCHITECTURE.md §5](../../docs/SERVICE_ARCHITECTURE.md), promoted to the
primary path — and per §5 it is disclosed rather than glossed.

## Model access is per-account, and ours is narrow

`us.anthropic.claude-sonnet-4-6` — the model the architecture baseline records
as working — is **denied** under the credentials now in `.env`:
`AccessDeniedException`, missing `aws-marketplace:Subscribe`. That earlier probe
ran under a different credential. Invokable today:

- `openai.gpt-oss-120b-1:0` — **the default**
- `us.amazon.nova-pro-v1:0` — also supports tool use
- `us.meta.llama3-3-70b-instruct-v1:0` — also supports tool use

gpt-oss is the default on measured behaviour. Given the `demo-1` trial window,
Nova recommended reverting to v1 and did not notice that v1's own p95 of 339 ms
breaches the 300 ms threshold. gpt-oss recommended `hold` and listed all three
breaches — v1 latency, v2 latency, v2 success rate — which is the D9 finding
arrived at from the evidence rather than from being told.

It returns a `reasoningContent` block next to the text block. That is handled:
`text_of` selects blocks containing `text`, so reasoning never reaches output,
and the tool loop passes the block back unchanged because the model needs it.

Override with `BEDROCK_MODEL_ID`. Run `preflight` before the demo; it probes
each id rather than assuming, because subscriptions change under you.

## What keeps them advisory

D8 is enforced, not just asserted. `ledger.py` refuses any event outside
`{agent_advisory, agent_narrative}` and refuses a payload carrying `verdict`,
`contract_hash`, `route_uid` or `reason`. The narrator refuses to run without a
verdict already in the ledger. No agent imports the executor or holds a
kubeconfig.

The honest limit: with the controller key an agent could write any document. The
guard removes the accident, not the adversary. Independence here is
credential-level and code-level, not cryptographic.

Queries run deterministically for the investigator and narrator — the model
picks nothing, it reasons over rows from the same `.esql` files the verifier
reads. That is what makes "the agent's numbers match the verifier's numbers"
true by construction rather than by luck. Only the briefer lets the model choose
a query, because a judge's question is not known in advance.

## A model weakness worth knowing about

Nova sums rows by hand and gets it wrong. Asked for a run total it answered
35,990 twice in a row, confidently — ten short of the real 36,000 — and telling
it not to do arithmetic in the system prompt did not stop it. gpt-oss gets the
same sums right, but the fix stays in place because it is cheap and the next
model swap should not have to rediscover this. `esql.py` now
computes `column_totals` for count columns (`requests`, `successes` only; summing
a rate or a percentile is meaningless) and hands them over, after which both runs
said 36,000. If you add a query whose counts an agent might total, add its column
to `SUMMABLE` rather than trusting the model.

Every result carries `query_id`s (`window-metrics@53cfadf2f6d1`) derived from
the query text and bound parameters, so any claim can be re-run and checked.

## Credentials

Reads use `ELASTIC_VERIFIER_API_KEY` when it exists, falling back to the
unrestricted setup key with a warning on every run. Writes use
`ELASTIC_CONTROLLER_API_KEY` the same way. Run `elastic/create-api-keys.sh` to
get the scoped keys; until then the separation the architecture claims is not
actually in force, which is what the warning says.
