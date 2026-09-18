#!/usr/bin/env bash
# Explicitly start a staged rollout. Each adjacent step requires a fresh PROVED
# verdict, unchanged config/route, and a new frozen trial. No blind rollback.
set -euo pipefail
cd "$(dirname "$0")/.."
[[ $# == 2 ]] || { echo 'usage: scripts/rollout.sh healthy|bad-recovery <unique-prefix>' >&2; exit 1; }
profile="$1"
prefix="$2"
[[ "$profile" == healthy || "$profile" == bad-recovery ]] || exit 1
[[ "$prefix" =~ ^[a-z0-9][a-z0-9-]{0,34}$ ]] || { echo 'Use a DNS-safe prefix of at most 35 characters' >&2; exit 1; }
export SLOPROOF_ELASTIC_REVIEW="${SLOPROOF_ELASTIC_REVIEW:-1}"
previous=''
for weight in 10 25 50 100; do
  run="${prefix}-s${weight}"
  args=("$profile" "$run")
  if [[ -n "$previous" ]]; then args+=(--from-run "$previous"); fi
  if scripts/run-trial.sh "${args[@]}"; then
    previous="$run"
  else
    status=$?
    echo "Rollout paused at stage $weight. No further stage or automatic reversal will run. Inspect evidence/$run-verdict.json and sloproof-loop." >&2
    exit "$status"
  fi
done
echo 'The 100% stage met the contract at the measured generated load. Rollout plan complete; continue monitoring.'
