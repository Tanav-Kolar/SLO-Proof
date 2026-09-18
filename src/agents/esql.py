"""Runs the saved ES|QL evidence queries and gives each run a citable id.

The agents never compose ES|QL. They choose from the five queries in
elastic/queries/ and supply parameters, which is the same constraint Agent
Builder's esql tools impose and the reason the numbers an agent quotes are the
same numbers the verifier will compute: it is literally the same file.

Every execution returns a query_id derived from the query text and the bound
parameters. A recommendation that cites query_ids can therefore be checked --
re-run the id and you must get the same rows.
"""
import hashlib
import json
import pathlib
import urllib.error
import urllib.request

from config import REPO

QUERY_DIR = REPO / "elastic" / "queries"


def available() -> list[str]:
    return sorted(p.stem for p in QUERY_DIR.glob("*.esql"))


class QueryResult:
    def __init__(self, name: str, query: str, params: dict, columns: list[str], rows: list[list]):
        self.name = name
        self.query = query
        self.params = params
        self.columns = columns
        self.rows = rows
        digest = hashlib.sha256(
            (query + json.dumps(params, sort_keys=True)).encode()
        ).hexdigest()[:12]
        self.query_id = f"{name}@{digest}"

    def as_records(self) -> list[dict]:
        return [dict(zip(self.columns, row)) for row in self.rows]

    def for_model(self) -> dict:
        """The shape handed to the LLM. Rows as records so the model cannot
        mis-associate a value with the wrong column, plus the query_id it must
        cite and an explicit empty-result warning.

        The empty case matters: the contract says an empty result is missing
        evidence, never zero errors, and a model shown `[]` will otherwise
        cheerfully report perfect health."""
        payload = {
            "query_id": self.query_id,
            "query_name": self.name,
            "params": self.params,
            "row_count": len(self.rows),
            "rows": self.as_records(),
            "column_totals": self.totals(),
            "note": (
                "EMPTY RESULT. This is missing evidence, not zero errors. "
                "Any conclusion drawn from it must be INCONCLUSIVE."
                if not self.rows
                else None
            ),
        }
        return payload

    # Counts only. Summing a rate or a percentile across rows is meaningless,
    # and a model asked for a total will otherwise add the columns it happens to
    # see. Observed: the briefer summed 10,755 + 20,196 + 5,049 to 35,990 twice
    # in a row -- ten short, stated with complete confidence. Hand it the
    # arithmetic rather than asking it not to do arithmetic.
    SUMMABLE = {"requests", "successes"}

    def totals(self) -> dict:
        sums = {}
        for i, column in enumerate(self.columns):
            if column not in self.SUMMABLE:
                continue
            values = [row[i] for row in self.rows if isinstance(row[i], (int, float))]
            if values:
                sums[column] = sum(values)
        return sums or None


def run(cfg, name: str, **params) -> QueryResult:
    path = QUERY_DIR / f"{name}.esql"
    if not path.exists():
        raise ValueError(f"no such query {name!r}; have {available()}")
    query = path.read_text()

    body = json.dumps(
        {"query": query, "params": [{k: v} for k, v in params.items()]}
    ).encode()
    req = urllib.request.Request(
        cfg.es_endpoint + "/_query",
        data=body,
        method="POST",
        headers={
            "Authorization": "ApiKey " + cfg.read_key,
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=90) as response:
            payload = json.load(response)
    except urllib.error.HTTPError as exc:
        raise RuntimeError(f"ES|QL {name} failed: {exc.read().decode()[:600]}") from None

    return QueryResult(
        name,
        query,
        params,
        [c["name"] for c in payload["columns"]],
        payload["values"],
    )
