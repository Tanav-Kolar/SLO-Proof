"""Agent 1 of 3 -- Investigator. Recommends; never decides.

It reads one evaluation window through the same three ES|QL files the verifier
will read, and returns a structured recommendation. The recommendation is
written to the ledger as `agent_advisory` and has no path to the executor.

The queries are run deterministically rather than chosen by the model. That is
the whole reason this agent can satisfy Person 3's blocking gate -- "agent cites
measurements that match the verifier's numbers" -- because the measurements are
not a second opinion about the data, they are the same query output.
"""
import json

import esql
from bedrock import extract_json

ACTIONS = ["promote", "hold", "revert", "escalate", "inconclusive"]

SYSTEM = """You are the SLOProof Investigator. You advise; a deterministic verifier decides.

You are given the output of ES|QL queries run against client-observed request
records. Every number you state must come from those rows. Do not estimate,
interpolate, or recall anything from outside the evidence block.

Contract in force:
  success_rate_min      0.99
  p95_latency_ms_max    300 ms
  min_samples_per_window 400 ordinary requests per version
  a timeout counts as a failure
  an empty result is MISSING EVIDENCE, never zero errors

Rules you must follow:
- In this recovery demo, v1 is the recovery candidate and v2 is the incident
  version. These roles are explicit; do not infer them from version numbering.
- A passing bounded trial supports operator review only. It does not prove
  full-load recovery or authorize an automatic traffic change. A failed
  candidate can still be better than v2; failure does not order reversal to v2.
- If any version in the window has fewer than 400 ordinary requests, or a query
  returned no rows, or config_hashes is not 1, the only permitted action is
  "inconclusive".
- Never state or imply a verdict. PROVED, FAILED and INCONCLUSIVE are the
  verifier's words, not yours. Say what you recommend and why.
- Cite the query_id behind every claim.

Reply with one JSON object and nothing else:
{
  "action": one of %s,
  "target_version": string or null,
  "proposed_weights": {"<version>": integer, ...} or null,
  "confidence": "high" | "medium" | "low",
  "rationale": one paragraph, plain English, naming the measured values,
  "measurements": [{"version":..., "requests":..., "success_rate":..., "p95_ms":...}],
  "cited_query_ids": [string, ...],
  "blocking_concerns": [string, ...]
}""" % json.dumps(ACTIONS)


def investigate(cfg, brain, *, run_id: str, window_start: str, window_end: str) -> dict:
    completeness = esql.run(cfg, "evidence-completeness", run_id=run_id,
                            window_start=window_start, window_end=window_end)
    metrics = esql.run(cfg, "window-metrics", run_id=run_id,
                       window_start=window_start, window_end=window_end)
    split = esql.run(cfg, "traffic-split", run_id=run_id,
                     window_start=window_start, window_end=window_end)
    results = [completeness, metrics, split]

    evidence = {
        "run_id": run_id,
        "window_start": window_start,
        "window_end": window_end,
        "queries": [r.for_model() for r in results],
    }
    response = brain.converse(
        system=SYSTEM,
        messages=[{"role": "user", "content": [{"text": json.dumps(evidence, indent=2)}]}],
        max_tokens=1600,
    )
    recommendation = extract_json(brain.text_of(response))

    ran = {r.query_id for r in results}
    recommendation["_validation"] = validate(recommendation, ran)
    recommendation["_evidence"] = {
        "query_ids": sorted(ran),
        "window_start": window_start,
        "window_end": window_end,
    }
    recommendation["_model"] = brain.model_id
    return recommendation


def validate(recommendation: dict, ran: set[str]) -> dict:
    """Check the model against the evidence it was given.

    Two failure modes matter and both are silent otherwise: an invented
    query_id, which makes a claim untraceable, and an action other than
    "inconclusive" on a window that did not meet minimum samples. Neither is
    corrected here -- the advisory is recorded as the model produced it, with
    the problems attached, because quietly rewriting an agent's output would
    make the ledger a worse record than no ledger.
    """
    problems = []

    action = recommendation.get("action")
    if action not in ACTIONS:
        problems.append(f"action {action!r} is not one of {ACTIONS}")

    cited = recommendation.get("cited_query_ids") or []
    invented = [q for q in cited if q not in ran]
    if invented:
        problems.append(f"cites query_ids that were never run: {invented}")
    if not cited:
        problems.append("cites no query_id, so no claim in it is traceable")

    for measurement in recommendation.get("measurements") or []:
        requests = measurement.get("requests")
        if isinstance(requests, (int, float)) and requests < 400 and action != "inconclusive":
            problems.append(
                f"version {measurement.get('version')} has {requests} requests "
                f"(below the 400 minimum) but the action is {action!r}"
            )

    return {"ok": not problems, "problems": problems}
