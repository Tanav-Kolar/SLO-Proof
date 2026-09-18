"""The agents' only write path, and the enforcement point for D8.

D8 says agent output is advisory. That is not a convention here, it is a check:
write() refuses any event outside ADVISORY_EVENTS, and strips the fields that
belong to the deterministic control plane. If someone later points an agent at
this module and asks it to record a verdict, it fails loudly instead of
producing a ledger entry that looks authoritative.

Independence is still credential-level, not cryptographic: with the controller
key an agent could in principle write any document. This guard removes the
accident, not the adversary. Say that plainly rather than overclaiming.
"""
import json
import sys
import urllib.error
import urllib.request

ADVISORY_EVENTS = {"agent_advisory", "agent_narrative"}

# Fields owned by the verifier and the executor. An agent record must never
# carry them, or a reader scanning the ledger could mistake advice for a ruling.
FORBIDDEN_FIELDS = {"verdict", "contract_hash", "route_uid", "reason"}

INDEX = "sloproof-ledger"


def write(cfg, *, event: str, run_id: str, actor: str, detail: dict,
          timestamp: str, window_start: str | None = None,
          window_end: str | None = None) -> str:
    if event not in ADVISORY_EVENTS:
        raise ValueError(
            f"REFUSING to write event {event!r}. The reasoning plane may write "
            f"only {sorted(ADVISORY_EVENTS)}. Verdicts belong to the verifier."
        )
    leaked = FORBIDDEN_FIELDS & set(detail)
    if leaked:
        raise ValueError(
            f"REFUSING: advisory detail carries control-plane fields {sorted(leaked)}."
        )

    doc = {
        "@timestamp": timestamp,
        "run_id": run_id,
        "event": event,
        "actor": actor,
        "detail": detail,
    }
    if window_start:
        doc["window_start"] = window_start
    if window_end:
        doc["window_end"] = window_end

    req = urllib.request.Request(
        f"{cfg.es_endpoint}/{INDEX}/_doc?refresh=wait_for",
        data=json.dumps(doc).encode(),
        method="POST",
        headers={
            "Authorization": "ApiKey " + cfg.write_key,
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            return json.load(response)["_id"]
    except urllib.error.HTTPError as exc:
        sys.exit(f"ledger write failed: {exc.read().decode()[:400]}")


def read_verdict(cfg, run_id: str) -> dict | None:
    """Find the most recent verdict the verifier wrote for this run.

    Returns None when there is none. The narrator must then refuse rather than
    narrate a verdict that does not exist -- inventing one would be exactly the
    failure this project exists to argue against.
    """
    body = json.dumps({
        "size": 1,
        "sort": [{"@timestamp": "desc"}],
        "query": {"bool": {"filter": [
            {"term": {"run_id": run_id}},
            {"exists": {"field": "verdict"}},
        ]}},
    }).encode()
    req = urllib.request.Request(
        f"{cfg.es_endpoint}/{INDEX}/_search",
        data=body,
        method="POST",
        headers={
            "Authorization": "ApiKey " + cfg.read_key,
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            hits = json.load(response)["hits"]["hits"]
    except urllib.error.HTTPError as exc:
        sys.exit(f"ledger read failed: {exc.read().decode()[:400]}")
    return hits[0]["_source"] if hits else None
