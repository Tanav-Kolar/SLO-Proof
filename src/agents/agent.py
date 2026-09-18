#!/usr/bin/env python3
"""SLOProof reasoning plane -- three advisory agents on Bedrock Converse.

    agent.py preflight
    agent.py investigate --run-id demo-1 --window-start ... --window-end ...
    agent.py narrate     --run-id demo-1 [--verdict-file v.json]
    agent.py ask         --run-id demo-1 "did v1 hold 99% during the trial?"

Run it through scripts/agent.sh, which supplies the virtualenv and .env.

Why this exists rather than Agent Builder: Agent Builder itself works -- tools,
agents and converse were all verified against the live project. What does not
work is Kibana's .bedrock connector, whose credential form has no session-token
field, so the temporary STS credentials this account uses (ASIA...) are rejected
with 403 "security token is invalid". Calling Bedrock directly takes the session
token, which is the whole difference.
"""
import argparse
import datetime
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import briefer
import esql
import investigator
import ledger
import narrator
from bedrock import Bedrock
from config import Config


def now() -> str:
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def cmd_preflight(cfg, args) -> int:
    """Check every dependency before the demo, not during it.

    Model access is the one that bites: Bedrock model ids are per-account
    subscriptions, so an id that worked under one credential can be denied under
    another with no code change at all.
    """
    import boto3
    from botocore.exceptions import ClientError

    ok = True
    print(f"elasticsearch  {cfg.es_endpoint}")
    print(f"  read key     {cfg.read_key_source}")
    print(f"  write key    {cfg.write_key_source}")
    try:
        result = esql.run(cfg, "outcome-breakdown", run_id=args.run_id or "demo-1")
        print(f"  query path   ok ({len(result.rows)} rows for run_id={args.run_id or 'demo-1'})")
    except Exception as exc:
        ok = False
        print(f"  query path   FAILED: {exc}")

    print(f"\nbedrock        region {cfg.region}, default model {cfg.model_id}")
    client = boto3.client("bedrock-runtime", region_name=cfg.region)
    candidates = [cfg.model_id, "us.amazon.nova-pro-v1:0",
                  "us.meta.llama3-3-70b-instruct-v1:0", "openai.gpt-oss-120b-1:0",
                  "us.anthropic.claude-sonnet-4-6"]
    seen, invokable = set(), []
    for model in candidates:
        if model in seen:
            continue
        seen.add(model)
        try:
            client.converse(
                modelId=model,
                messages=[{"role": "user", "content": [{"text": "OK"}]}],
                inferenceConfig={"maxTokens": 8},
            )
            invokable.append(model)
            print(f"  {model:46s} invokable")
        except ClientError as exc:
            print(f"  {model:46s} {exc.response.get('Error', {}).get('Code')}")
    if cfg.model_id not in invokable:
        ok = False
        print(f"\n  the configured model {cfg.model_id} is NOT invokable.")
        if invokable:
            print(f"  set BEDROCK_MODEL_ID={invokable[0]}")
    cfg.warn_unscoped(sys.stdout)
    return 0 if ok else 1


def cmd_investigate(cfg, args) -> int:
    brain = Bedrock(cfg)
    result = investigator.investigate(
        cfg, brain, run_id=args.run_id,
        window_start=args.window_start, window_end=args.window_end)
    print(json.dumps(result, indent=2))

    validation = result["_validation"]
    if not validation["ok"]:
        print("\nADVISORY HAS PROBLEMS:", file=sys.stderr)
        for problem in validation["problems"]:
            print(f"  - {problem}", file=sys.stderr)

    if not args.no_write:
        doc_id = ledger.write(
            cfg, event="agent_advisory", run_id=args.run_id, actor="investigator",
            detail=result, timestamp=now(),
            window_start=args.window_start, window_end=args.window_end)
        print(f"\nledger: agent_advisory {doc_id}", file=sys.stderr)
    return 0 if validation["ok"] else 2


def cmd_narrate(cfg, args) -> int:
    if args.verdict_file:
        verdict = json.loads(pathlib.Path(args.verdict_file).read_text())
    else:
        verdict = ledger.read_verdict(cfg, args.run_id)
    if not verdict:
        sys.exit(
            f"REFUSING: no verdict in the ledger for run {args.run_id!r}.\n"
            f"The narrator explains a verdict; it does not produce one. Run the\n"
            f"verifier first, or pass one with --verdict-file."
        )

    brain = Bedrock(cfg)
    result = narrator.narrate(cfg, brain, run_id=args.run_id, verdict=verdict)
    print(result["narrative"])
    print(f"\ncited: {', '.join(result['cited_query_ids'])}", file=sys.stderr)

    if not args.no_write:
        doc_id = ledger.write(cfg, event="agent_narrative", run_id=args.run_id,
                              actor="narrator", detail=result, timestamp=now())
        print(f"ledger: agent_narrative {doc_id}", file=sys.stderr)
    return 0


def cmd_ask(cfg, args) -> int:
    brain = Bedrock(cfg)
    result = briefer.ask(cfg, brain, run_id=args.run_id, question=" ".join(args.question))
    print(result["answer"])
    cited = result["cited_query_ids"]
    print(f"\ncited: {', '.join(cited) if cited else 'NOTHING -- treat this answer as unsupported'}",
          file=sys.stderr)
    return 0 if cited else 2


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)

    pre = sub.add_parser("preflight", help="check Elasticsearch, keys, and model access")
    pre.add_argument("--run-id", default="demo-1")
    pre.set_defaults(func=cmd_preflight)

    inv = sub.add_parser("investigate", help="advisory recommendation for one window")
    inv.add_argument("--run-id", required=True)
    inv.add_argument("--window-start", required=True, help="ISO-8601 UTC, inclusive")
    inv.add_argument("--window-end", required=True, help="ISO-8601 UTC, exclusive")
    inv.add_argument("--no-write", action="store_true", help="do not write to the ledger")
    inv.set_defaults(func=cmd_investigate)

    nar = sub.add_parser("narrate", help="explain a verdict the verifier already wrote")
    nar.add_argument("--run-id", required=True)
    nar.add_argument("--verdict-file", help="verdict JSON, when the ledger has none yet")
    nar.add_argument("--no-write", action="store_true")
    nar.set_defaults(func=cmd_narrate)

    ask = sub.add_parser("ask", help="free-text question over the evidence")
    ask.add_argument("--run-id", required=True)
    ask.add_argument("question", nargs="+")
    ask.set_defaults(func=cmd_ask)

    args = parser.parse_args()
    cfg = Config()
    if args.command != "preflight":
        cfg.warn_unscoped()
    return args.func(cfg, args)


if __name__ == "__main__":
    sys.exit(main())
