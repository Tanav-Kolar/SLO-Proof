#!/usr/bin/env bash
# Runs a SLOProof agent with the right interpreter and environment.
#
#   scripts/agent.sh preflight
#   scripts/agent.sh investigate --run-id demo-1 \
#       --window-start 2026-09-18T06:28:42Z --window-end 2026-09-18T06:31:12Z
#   scripts/agent.sh ask --run-id demo-1 "what fraction of v2 requests failed?"
#
# Creates .venv on first use. AWS credentials come from .env, including
# AWS_SESSION_TOKEN -- which is exactly why this path works where the Kibana
# .bedrock connector does not: that form has no session-token field.
set -euo pipefail
cd "$(dirname "$0")/.."

[[ -f .env ]] || { echo "REFUSING: no .env" >&2; exit 1; }

if [[ ! -x .venv/bin/python ]]; then
  echo "creating .venv (one time, ~15s)..." >&2
  python3 -m venv .venv
  .venv/bin/pip -q install -r src/agents/requirements.txt
fi

set -a; source .env; set +a
exec .venv/bin/python src/agents/agent.py "$@"
