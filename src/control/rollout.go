package control

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"time"
)

// Verdicts describe evidence. This separate, deterministic policy describes
// progression through an operator-started plan; it never executes an AI answer.
var RolloutWeights = []int{10, 25, 50, 100}

type EvidenceRetry struct {
	Allowed      bool   `json:"allowed"`
	DelaySeconds int    `json:"delay_seconds"`
	MaxRetries   int    `json:"max_additional_attempts"`
	SameWindows  bool   `json:"same_frozen_windows"`
	Reason       string `json:"reason"`
}

type CohortComparison struct {
	Window           int     `json:"window_index"`
	Start            string  `json:"window_start"`
	End              string  `json:"window_end"`
	CandidateP95     float64 `json:"candidate_p95_ms"`
	CandidateSuccess float64 `json:"candidate_success_rate"`
	OriginalP95      float64 `json:"original_p95_ms"`
	OriginalSuccess  float64 `json:"original_success_rate"`
	OriginalOutcome  string  `json:"original_window_outcome"`
}

type RolloutDecision struct {
	RunID             string             `json:"run_id"`
	Hash              string             `json:"contract_hash"`
	SourceID          string             `json:"source_verdict_id,omitempty"`
	SourceAt          string             `json:"source_verified_at"`
	Verdict           string             `json:"recorded_verdict"`
	Action            string             `json:"action"`
	Signal            string             `json:"signal"`
	Explanation       string             `json:"explanation"`
	Execution         string             `json:"execution"`
	Plan              []int              `json:"candidate_weight_plan"`
	CurrentWeight     int                `json:"current_candidate_weight"`
	NextWeight        int                `json:"next_candidate_weight,omitempty"`
	BothUnhealthy     bool               `json:"both_versions_unhealthy"`
	SharedCauseProven bool               `json:"shared_cause_proven"`
	Comparisons       []CohortComparison `json:"window_comparisons,omitempty"`
	Retry             EvidenceRetry      `json:"evidence_retry"`
	RetryExhausted    bool               `json:"retry_budget_exhausted"`
}

func RetryEvidence(v Verdict) EvidenceRetry {
	r := EvidenceRetry{DelaySeconds: 30, MaxRetries: 2, SameWindows: true, Reason: "Repair the evidence problem before a new trial; do not retry the route patch."}
	if v.Verdict != "INCONCLUSIVE" {
		r.Reason = "A conclusive result is never retried to search for a better outcome."
		return r
	}
	// A completed/reconciled cohort with too few samples cannot gain samples by
	// waiting. Missing receipts, changed identities and malformed evidence also
	// require repair, not an automatic execution retry.
	reasons := []string{v.Reason}
	if v.Reason == "missing, stale, changed or underpowered evidence" && len(v.Windows) > 0 {
		reasons = nil // Classify the actual window causes, not the umbrella label.
	}
	for _, w := range v.Windows {
		if w.Verdict == "INCONCLUSIVE" {
			reasons = append(reasons, w.Reason)
		}
	}
	joined := strings.Join(reasons, " ")
	for _, permanent := range []string{"HTTP 401", "HTTP 403", "HTTP 404", "config", "receipt", "anchor", "underpowered", "integrity", "invalid", "changed", "truncated", "unknown_version", "forbidden", "scope"} {
		if strings.Contains(joined, permanent) {
			return r
		}
	}
	// Every failing reason must be explicitly transient. One transient error
	// cannot hide a second, unknown failure.
	r.Allowed = len(reasons) > 0
	for _, reason := range reasons {
		transientReason := false
		for _, transient := range []string{"HTTP 429", "HTTP 502", "HTTP 503", "HTTP 504", "Client.Timeout", "context deadline exceeded", "window or ingestion allowance not closed", "partial ES|QL response", "partial reconciliation count"} {
			transientReason = transientReason || strings.Contains(reason, transient)
		}
		r.Allowed = r.Allowed && transientReason
	}
	if strings.Contains(v.Reason, "unreconciled generator evidence") {
		b, _ := json.Marshal(v.Reconciliation)
		var rec struct {
			Generator  Summary `json:"generator"`
			Searchable int64   `json:"searchable_now"`
		}
		if json.Unmarshal(b, &rec) == nil && rec.Generator.Validate(v.RunID, rec.Generator.Completed) == nil && rec.Searchable >= 0 && rec.Searchable < rec.Generator.Completed {
			r.Allowed = true
		}
	}
	if r.Allowed {
		r.Reason = "Hold the current split; retry only the same frozen evidence after 30 seconds, at most twice. Escalate if still inconclusive."
	}
	return r
}

func nextWeight(current int) int {
	for i, weight := range RolloutWeights {
		if weight == current && i+1 < len(RolloutWeights) {
			return RolloutWeights[i+1]
		}
	}
	return 0
}

func DecideRollout(v Verdict, c Contract) RolloutDecision {
	d := RolloutDecision{RunID: v.RunID, Hash: v.Hash, SourceAt: v.At, Verdict: v.Verdict,
		Action: "HOLD_AND_REPAIR", Signal: "EVIDENCE_INSUFFICIENT", Plan: append([]int{}, RolloutWeights...),
		Execution: "POLICY_ONLY_NO_ROUTING_PERFORMED", Retry: RetryEvidence(v)}
	if len(v.Receipt.Weights) == 2 {
		d.CurrentWeight = v.Receipt.Weights[0]
	}
	switch v.Verdict {
	case "PROVED":
		if len(v.Windows) != c.Observation.Count {
			d.Explanation = "Recorded pass lacks all required windows; progression is blocked."
			return d
		}
		for _, w := range v.Windows {
			if w.Verdict != "PROVED" || w.Requests < float64(c.Eligibility.Minimum) || !finite(w.Success) || !finite(w.P95) || w.Success < c.Criteria.Success || w.Success > 1 || w.P95 < 0 || w.P95 >= c.Criteria.P95 {
				d.Explanation = "Recorded pass is inconsistent with its window measurements; progression is blocked."
				return d
			}
		}
		d.NextWeight = nextWeight(d.CurrentWeight)
		if d.NextWeight > 0 {
			d.Action, d.Signal = "CONTINUE_PLAN", "STAGE_CONTRACT_MET"
			d.Explanation = fmt.Sprintf("Continue the operator-started plan from %d%% to %d%% candidate traffic only after freshness, configuration and route-conflict checks. Freeze and measure a new stage before any further expansion.", d.CurrentWeight, d.NextWeight)
		} else if d.CurrentWeight == 100 {
			d.Action, d.Signal = "PLAN_COMPLETE", "FULL_WEIGHT_STAGE_MET"
			d.Explanation = "The final 100% traffic stage met the contract at the generated load used for this trial. Continue monitoring; this is not a guarantee at untested production loads."
		} else {
			d.Explanation = "Recorded weights do not match the fixed rollout plan."
		}
	case "FAILED":
		d.Action, d.Signal = "PAUSE_AND_INVESTIGATE", "CANDIDATE_BREACH"
		d.Explanation = "Pause rollout progression. The candidate misses the contract. Keep current weights and investigate; do not automatically return to another unhealthy version."
		completeComparison := len(v.Windows) == c.Observation.Count
		originalFailed := false
		for _, w := range v.Windows {
			outcome, p95, success := compareOriginal(c, w)
			if outcome == "INCONCLUSIVE" {
				completeComparison = false
				continue
			}
			d.Comparisons = append(d.Comparisons, CohortComparison{w.Index, w.Start, w.End, w.P95, w.Success, p95, success, outcome})
			originalFailed = originalFailed || outcome == "FAILED"
		}
		if completeComparison && originalFailed {
			d.BothUnhealthy, d.Signal = true, "BOTH_VERSIONS_BREACH"
			d.Explanation = "Both candidate and original breach the contract in the recorded windows. Pause expansion and investigate common dependencies, infrastructure and configuration. A shared cause is a hypothesis, not a proven diagnosis; a relative improvement does not meet the absolute target."
		}
	case "INCONCLUSIVE":
		d.Explanation = "Hold current weights and block expansion. " + d.Retry.Reason
		if d.Retry.Allowed {
			d.Action, d.Signal = "HOLD_AND_RECHECK", "TRANSIENT_EVIDENCE_DELAY"
		}
	default:
		d.Explanation = "Unknown result; no progression is permitted."
	}
	return d
}

func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

func compareOriginal(c Contract, w Window) (string, float64, float64) {
	var complete, metrics, integrity []map[string]any
	for _, q := range w.Queries {
		if q.Error != "" || q.Result.IsPartial {
			return "INCONCLUSIVE", 0, 0
		}
		switch q.Name {
		case "evidence-completeness":
			complete = originalRows(q.Result.Rows())
		case "window-metrics":
			metrics = originalRows(q.Result.Rows())
		case "evidence-integrity":
			integrity = q.Result.Rows()
		}
	}
	start, err := time.Parse(time.RFC3339Nano, w.Start)
	if err != nil {
		return "INCONCLUSIVE", 0, 0
	}
	end, err := time.Parse(time.RFC3339Nano, w.End)
	if err != nil {
		return "INCONCLUSIVE", 0, 0
	}
	// Apply the same count, coverage and metric checks to v2. This comparison
	// does not invent a second frozen deployment/configuration attestation.
	outcome, _, _, success, p95 := EvaluateWindow(c, start, end, complete, metrics, integrity)
	return outcome, p95, success
}

func originalRows(rows []map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, row := range rows {
		if Text(row, "version") != "v2" {
			continue
		}
		copy := map[string]any{}
		for key, value := range row {
			copy[key] = value
		}
		copy["version"] = "v1"
		out = append(out, copy)
	}
	return out
}

func ReadRecordedVerdict(e Elastic, run string) (string, Verdict, error) {
	var response struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				ID     string `json:"_id"`
				Source struct {
					RunID   string  `json:"run_id"`
					Hash    string  `json:"contract_hash"`
					Verdict string  `json:"verdict"`
					Detail  Verdict `json:"detail"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := ValidateID(run); err != nil {
		return "", Verdict{}, err
	}
	filters := []any{map[string]any{"term": map[string]any{"run_id": run}}, map[string]any{"term": map[string]any{"event": "verdict"}}, map[string]any{"term": map[string]any{"actor": "deterministic-verifier"}}}
	err := e.Do("POST", "/sloproof-ledger/_search", map[string]any{"size": 2, "track_total_hits": true, "query": map[string]any{"bool": map[string]any{"filter": filters}}}, &response)
	if err != nil {
		return "", Verdict{}, err
	}
	if response.Hits.Total.Value != 1 || len(response.Hits.Hits) != 1 {
		return "", Verdict{}, fmt.Errorf("expected exactly one deterministic verdict")
	}
	hit := response.Hits.Hits[0]
	v := hit.Source.Detail
	if hit.Source.RunID != run || v.RunID != run || v.Hash != hit.Source.Hash || v.Verdict != hit.Source.Verdict {
		return "", v, fmt.Errorf("inconsistent recorded verdict")
	}
	return hit.ID, v, nil
}

func RecordRolloutPolicy(e Elastic, f Frozen, sourceID string, v Verdict) (RolloutDecision, error) {
	c, _, err := f.ValidateLocal("contracts/checkout-slo.json", "elastic/queries")
	if err != nil {
		return RolloutDecision{}, err
	}
	if v.RunID != f.RunID || v.Hash != f.Hash {
		return RolloutDecision{}, fmt.Errorf("policy source mismatch")
	}
	d := DecideRollout(v, c)
	d.SourceID = sourceID
	var history struct {
		RunID     string `json:"run_id"`
		Final     string `json:"final_verdict"`
		Exhausted bool   `json:"retry_budget_exhausted"`
	}
	if raw, err := os.ReadFile("evidence/" + v.RunID + "-verification-retries.json"); err == nil {
		if err = json.Unmarshal(raw, &history); err != nil || history.RunID != v.RunID || history.Final != v.Verdict {
			return d, fmt.Errorf("retry history does not match the final decision")
		}
		if history.Exhausted && v.Verdict == "INCONCLUSIVE" {
			d.Action, d.Signal, d.RetryExhausted = "HOLD_AND_ESCALATE", "RETRY_BUDGET_EXHAUSTED", true
			d.Retry.Allowed = false
			d.Explanation = "The bounded evidence retry budget is exhausted. Hold current traffic, block further progression and escalate to the operator. Do not restart the retry budget on another workflow review."
		}
	} else if !os.IsNotExist(err) {
		return d, err
	}
	// Deterministic ID: repeated review cannot replace the policy or decision.
	path := "/sloproof-ledger/_doc/" + v.RunID + ".rollout-policy"
	var old struct {
		Source struct {
			Detail RolloutDecision `json:"detail"`
		} `json:"_source"`
	}
	if err := e.Do("GET", path, nil, &old); err == nil {
		if !reflect.DeepEqual(old.Source.Detail, d) {
			return d, fmt.Errorf("existing rollout policy differs; refusing overwrite")
		}
		return d, nil
	} else if !strings.Contains(err.Error(), "HTTP 404:") {
		return d, err
	}
	doc := map[string]any{"@timestamp": Now(), "run_id": v.RunID, "event": "rollout_policy", "actor": "deterministic-rollout-policy", "contract_hash": v.Hash, "verdict": v.Verdict, "reason": d.Signal, "detail": d}
	err = e.Do("PUT", "/sloproof-ledger/_create/"+v.RunID+".rollout-policy?refresh=wait_for", doc, nil)
	return d, err
}

// ValidateAdvance authorizes just one adjacent stage. It rejects using an old
// demo pass, changing config, replaying a stage, or overwriting another actor.
func ValidateAdvance(prior Frozen, v Verdict, next Frozen, now time.Time) (int, error) {
	c, err := ParseContract([]byte(prior.Source))
	if err != nil {
		return 0, err
	}
	if v.RunID != prior.RunID || v.Hash != prior.Hash || next.Hash != prior.Hash || next.RunID == prior.RunID || next.ConfigHash != prior.ConfigHash {
		return 0, fmt.Errorf("stage identity, contract or candidate configuration changed")
	}
	if err := ValidateReceipt(prior, v.Receipt); err != nil {
		return 0, err
	}
	d := DecideRollout(v, c)
	if d.Action != "CONTINUE_PLAN" {
		return 0, fmt.Errorf("rollout progression blocked: %s", d.Action)
	}
	verified, err := time.Parse(time.RFC3339Nano, v.At)
	if err != nil || verified.After(now) || now.Sub(verified) > 15*time.Minute {
		return 0, fmt.Errorf("prior verdict is stale or invalid; fresh stage evidence required")
	}
	last, err := time.Parse(time.RFC3339Nano, v.Windows[len(v.Windows)-1].End)
	if err != nil || last.After(verified) || now.Sub(last) > 15*time.Minute {
		return 0, fmt.Errorf("prior observation windows are stale or invalid")
	}
	frozen, err := time.Parse(time.RFC3339Nano, next.FrozenAt)
	if err != nil || frozen.Before(verified) || frozen.After(now) {
		return 0, fmt.Errorf("next stage was not frozen after the prior verdict")
	}
	b, err := next.Route.Backends()
	if err != nil {
		return 0, err
	}
	if next.Route.Metadata.UID != v.Receipt.UID || next.Route.Metadata.RV != v.Receipt.After || b[0].Weight != v.Receipt.Weights[0] || b[1].Weight != v.Receipt.Weights[1] {
		return 0, fmt.Errorf("route changed since prior stage; pause and escalate")
	}
	return d.NextWeight, nil
}
