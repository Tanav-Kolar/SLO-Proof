"""Agent 3 of 3 -- Briefer. Answers free-text questions over the evidence.

This is the only one of the three that uses Converse tool use, because a judge's
question is not known in advance and the right query cannot be picked for it
ahead of time. The tools are the same five .esql files; the model chooses which
to run and with what parameters, and nothing else is reachable.

Read-only by construction: it is given no ledger writer.
"""
import esql

SYSTEM = """You are the SLOProof Briefer, answering questions about a recorded
incident from client-observed request evidence in Elasticsearch.

You have one tool. Use it. Never answer a question about the data from memory,
and never guess at a number you have not retrieved.

Guidance:
- v1 is the recovery candidate and v2 is the incident version in this demo.
  These are bounded trial observations, not proof of full-load recovery.
  Traffic expansion requires operator review; the agents do not change routing.
- window-metrics, traffic-split and evidence-completeness need window_start and
  window_end as ISO-8601 UTC, for example 2026-09-18T06:28:42Z.
- incident-timeline and outcome-breakdown take only run_id and always cover the
  WHOLE RUN. If the question names a time window, answer it with window-metrics
  or traffic-split. Never present a whole-run total as a window total; use the
  timeline's buckets if you need to describe change over time within a window.
- An empty result means the evidence is missing. Say so. It never means zero errors.
- The contract is 0.99 success rate, 300 ms p95, and at least 400 ordinary
  requests per version per window.
- You do not issue verdicts. If asked whether something passed, give the
  measured values against the thresholds and say the verifier owns the verdict.
- Do not do arithmetic. Quote retrieved values as they are. If a question needs a
  total or a percentage that no row contains, say which rows it would come from
  and that the number is not directly measured, rather than computing it.

Answer in at most four sentences, quoting the numbers you retrieved."""


def tool_spec() -> list:
    return [{"toolSpec": {
        "name": "run_evidence_query",
        "description": (
            "Run one of the saved SLOProof ES|QL evidence queries and return its rows. "
            "window-metrics: per-version requests, success_rate, p50 and p95 for a window. "
            "traffic-split: per-version request counts for a window, proving where traffic went. "
            "evidence-completeness: whether a window has enough samples and stable config. "
            "incident-timeline: per-version health in 15-second buckets across the run. "
            "outcome-breakdown: counts by version, outcome kind and HTTP status across the run."
        ),
        "inputSchema": {"json": {
            "type": "object",
            "properties": {
                "name": {"type": "string", "enum": esql.available()},
                "run_id": {"type": "string", "description": "run under examination, e.g. demo-1"},
                "window_start": {"type": "string", "description": "ISO-8601 UTC, inclusive. Required for window-metrics, traffic-split, evidence-completeness."},
                "window_end": {"type": "string", "description": "ISO-8601 UTC, exclusive. Required for the same three."},
            },
            "required": ["name", "run_id"],
        }},
    }}]


WINDOWED = {"window-metrics", "traffic-split", "evidence-completeness"}


def ask(cfg, brain, *, run_id: str, question: str) -> dict:
    def dispatch(tool_name, params):
        if tool_name != "run_evidence_query":
            raise ValueError(f"unknown tool {tool_name!r}")
        name = params["name"]
        # run_id is pinned by the operator, not by the model. A briefer that can
        # wander into another run's evidence mid-demo is a liability.
        call = {"run_id": run_id}
        if name in WINDOWED:
            for key in ("window_start", "window_end"):
                if not params.get(key):
                    raise ValueError(f"{name} requires {key} as ISO-8601 UTC")
                call[key] = params[key]
        result = esql.run(cfg, name, **call)
        payload = result.for_model()
        # Scope has to be stated in the tool result, not only in the system
        # prompt. Asked a window-shaped question, the model will otherwise
        # report whole-run totals from incident-timeline or outcome-breakdown as
        # though they were the window's -- observed, not hypothetical.
        payload["scope"] = (
            f"the window {call['window_start']} to {call['window_end']}"
            if name in WINDOWED
            else "THE WHOLE RUN, not any single window. Do not describe these "
                 "numbers as belonging to a window. If the question is about a "
                 "window, call a windowed query instead."
        )
        return payload, result.query_id

    answer, cited = brain.tool_loop(
        system=SYSTEM,
        question=f"Run {run_id}. Question: {question}",
        tools=tool_spec(),
        dispatch=dispatch,
    )
    return {"question": question, "answer": answer,
            "cited_query_ids": cited, "_model": brain.model_id}
