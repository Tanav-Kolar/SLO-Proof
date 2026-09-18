#!/usr/bin/env bash
# Drives the routing phases of a demo run against already-running load.
#
#   baseline   100/0   healthy service, establishes what "good" looks like
#   incident     0/100  all traffic to the faulty version
#   trial       10/90   a bounded slice back to the known-good version
#
# It only moves traffic. It does not decide anything: the verdict comes from the
# verifier reading Elasticsearch, and promotion is a separate explicit step.
#
# Usage: scripts/demo-sequence.sh [baseline_s] [incident_s] [trial_s]
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/_guard.sh
require_context

BASELINE="${1:-70}"
INCIDENT="${2:-90}"
TRIAL="${3:-180}"

phase() { printf '\n[%s] == %s ==\n' "$(date -u +%H:%M:%SZ)" "$1"; }

phase "baseline 100/0 for ${BASELINE}s"
scripts/set-weights.sh 100 0 | tail -3
sleep "$BASELINE"

phase "incident 0/100 for ${INCIDENT}s -- all traffic to the faulty version"
scripts/set-weights.sh 0 100 | tail -3
sleep "$INCIDENT"

phase "recovery trial 10/90 for ${TRIAL}s -- bounded slice back to v1"
scripts/set-weights.sh 10 90 | tail -3
sleep "$TRIAL"

phase "sequence complete -- weights left at the trial split"
echo "Routing is still 10/90. Promotion (100/0) is a separate, explicit act that"
echo "must follow a PROVED verdict from the verifier, not the end of this script."
