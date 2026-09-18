#!/usr/bin/env python3
"""Run the existing advisory agents on a saved live verdict and retain outputs."""
import argparse
import datetime
import json
import pathlib
import re
import subprocess

ROOT = pathlib.Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("run_id")
args = parser.parse_args()
if not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}", args.run_id):
    parser.error("invalid run ID")
verdict = json.loads((ROOT / "evidence" / f"{args.run_id}-verdict.json").read_text())
if verdict.get("run_id") != args.run_id or not verdict.get("windows"):
    parser.error("a real verdict with observation windows is required")
window = verdict["windows"][0]
output = ROOT / "evidence" / f"{args.run_id}-agents.json"
if output.exists():
    parser.error("agent evidence already exists for this run; retain the original")

def invoke(*arguments):
    completed = subprocess.run(
        [str(ROOT / "scripts/agent.sh"), *arguments], cwd=ROOT,
        text=True, capture_output=True, timeout=180,
    )
    if completed.returncode != 0:
        raise SystemExit(f"{arguments[0]} failed: {completed.stderr}")
    return {"stdout": completed.stdout.strip(), "diagnostics": completed.stderr.strip()}

investigation = invoke("investigate", "--run-id", args.run_id,
                       "--window-start", window["window_start"],
                       "--window-end", window["window_end"])
advisory = json.loads(investigation["stdout"])
if not advisory.get("_validation", {}).get("ok"):
    raise SystemExit("advisory validation failed")
narration = invoke("narrate", "--run-id", args.run_id)
briefing = invoke("ask", "--run-id", args.run_id,
                  "Compare v1 and v2 success and latency. Is this trial evidence "
                  "or proof of full-load recovery? Cite the queries you ran.")
payload = {
    "run_id": args.run_id,
    "recorded_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "scope": "AI advisory and explanation; no routing action",
    "investigator": advisory,
    "narrator": narration,
    "briefer": briefing,
    "investigator_diagnostics": investigation["diagnostics"],
}
output.write_text(json.dumps(payload, indent=2) + "\n")
print(f"Saved {output.name}: Investigator validation passed; Narrator and Briefer completed.")
