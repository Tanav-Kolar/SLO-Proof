"""Agent 2 of 3 -- Narrator. Explains a verdict that already exists.

Strictly post-hoc. It will not run unless a verdict is available, either from
the ledger or handed in with --verdict-file, because a narrator that can
describe an outcome before the outcome is decided is just the decision-maker
wearing a different label. That ordering is the point of the demo, so the guard
is a refusal rather than a warning.

It reads incident-timeline and outcome-breakdown for shape -- when the
regression started, whether failures were injected 500s or timeouts or requests
that never reached a pod -- and writes the result to the ledger as
`agent_narrative`.
"""
import json

import esql

SYSTEM = """You are the SLOProof Narrator. A deterministic verifier has already
returned a verdict. Your job is to explain it to a room of judges in a way that
a careful engineer would not object to.

You are given the verdict, and ES|QL output showing per-version health over time
in 15-second buckets plus a breakdown of failure kinds.

Rules:
- v1 is the recovery candidate and v2 is the incident version in this demo.
- A PROVED result covers the observed trial only. The operator still decides
  whether to expand traffic. No verdict automatically changes routing or
  closes the incident; a FAILED candidate can still improve on v2.
- The verdict is fixed. Explain it. Never argue with it, soften it, or restate
  it as a different word.
- Every number you use must appear in the evidence rows.
- Say which failure kind dominated and when the change is visible in the
  buckets. "Never reached a pod" is materially different from an injected 500
  and worth calling out, because service-side logging cannot see it.
- No speculation about root cause beyond what the rows support.

Write 3 to 5 sentences of plain prose. No headings, no bullets, no JSON."""


def narrate(cfg, brain, *, run_id: str, verdict: dict) -> dict:
    timeline = esql.run(cfg, "incident-timeline", run_id=run_id)
    outcomes = esql.run(cfg, "outcome-breakdown", run_id=run_id)
    results = [timeline, outcomes]

    # The timeline is one row per 15-second bucket per version and a long run
    # will not fit a sensible context. Keep the head and tail: the transition
    # into the incident and the state at the end are what the narrative needs.
    timeline_view = timeline.for_model()
    rows = timeline_view["rows"]
    if len(rows) > 80:
        timeline_view = dict(timeline_view)
        timeline_view["rows"] = rows[:40] + rows[-40:]
        timeline_view["note"] = (
            f"{len(rows)} buckets total; showing the first 40 and last 40. "
            f"Do not describe the omitted middle."
        )

    evidence = {
        "run_id": run_id,
        "verdict": verdict,
        "queries": [timeline_view, outcomes.for_model()],
    }
    response = brain.converse(
        system=SYSTEM,
        messages=[{"role": "user", "content": [{"text": json.dumps(evidence, indent=2)}]}],
        max_tokens=2400,
    )
    if response.get("stopReason") != "end_turn":
        raise RuntimeError("Narrator did not finish its answer; refusing to record a partial narrative")
    return {
        "narrative": brain.text_of(response),
        "verdict_narrated": verdict.get("verdict"),
        "cited_query_ids": [r.query_id for r in results],
        "_model": brain.model_id,
    }
