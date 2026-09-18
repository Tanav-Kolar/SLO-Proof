package control

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func savedTrial(t *testing.T, run string) (Frozen, Verdict, Contract) {
	t.Helper()
	var f Frozen
	var v Verdict
	for suffix, target := range map[string]any{"contract": &f, "verdict": &v} {
		b, err := os.ReadFile("../../evidence/" + run + "-" + suffix + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, target); err != nil {
			t.Fatal(err)
		}
	}
	c, err := ParseContract([]byte(f.Source))
	if err != nil {
		t.Fatal(err)
	}
	return f, v, c
}

func TestSavedTrialsProduceRequestedRolloutCases(t *testing.T) {
	for _, tc := range []struct{ run, action, signal string }{
		{"demo-healthy-20260918-01", "CONTINUE_PLAN", "STAGE_CONTRACT_MET"},
		{"demo-slow-20260918-02", "PAUSE_AND_INVESTIGATE", "BOTH_VERSIONS_BREACH"},
		{"demo-slow-20260918-01", "HOLD_AND_REPAIR", "EVIDENCE_INSUFFICIENT"},
	} {
		t.Run(tc.run, func(t *testing.T) {
			_, v, c := savedTrial(t, tc.run)
			before, _ := json.Marshal(v)
			d := DecideRollout(v, c)
			if d.Action != tc.action || d.Signal != tc.signal || d.SharedCauseProven || d.Execution != "POLICY_ONLY_NO_ROUTING_PERFORMED" {
				t.Fatalf("unexpected policy: %+v", d)
			}
			after, _ := json.Marshal(v)
			if string(before) != string(after) {
				t.Fatal("policy mutated original verifier evidence")
			}
		})
	}
}

func TestMissingOriginalEvidenceDoesNotProveSharedCause(t *testing.T) {
	_, v, c := savedTrial(t, "demo-slow-20260918-02")
	v.Windows[0].Queries = nil
	d := DecideRollout(v, c)
	if d.BothUnhealthy || d.SharedCauseProven || d.Signal != "CANDIDATE_BREACH" {
		t.Fatal(d)
	}
}

func TestBoundedRetryOnlyForRecoverableEvidence(t *testing.T) {
	for _, tc := range []struct {
		reason string
		want   bool
	}{
		{"Elasticsearch query: HTTP 503: unavailable", true},
		{"window or ingestion allowance not closed", true},
		{"partial ES|QL response", true},
		{"underpowered window", false},
		{"candidate config changed or missing", false},
		{"receipt anchor differs from observed traffic", false},
		{"Elasticsearch GET .effective: HTTP 404: missing", false},
		{"verifier has forbidden index on sloproof-requests", false},
		{"unrecognized failure", false},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			r := RetryEvidence(Verdict{Verdict: "INCONCLUSIVE", Reason: tc.reason})
			if r.Allowed != tc.want || r.MaxRetries != 2 || r.DelaySeconds != 30 || !r.SameWindows {
				t.Fatal(r)
			}
		})
	}
	for _, status := range []string{"FAILED", "PROVED"} {
		if RetryEvidence(Verdict{Verdict: status, Reason: "HTTP 503"}).Allowed {
			t.Fatal("retried conclusive result")
		}
	}
	v := Verdict{Verdict: "INCONCLUSIVE", Reason: "missing, stale, changed or underpowered evidence", Windows: []Window{{Verdict: "INCONCLUSIVE", Reason: "HTTP 503"}}}
	if !RetryEvidence(v).Allowed {
		t.Fatal("generic aggregate reason hid transient cause")
	}
	v.Windows = append(v.Windows, Window{Verdict: "INCONCLUSIVE", Reason: "unexpected failure"})
	if RetryEvidence(v).Allowed {
		t.Fatal("transient cause hid unknown failure")
	}
}

func TestRetryLagDoesNotMaskDroppedOrLostRecords(t *testing.T) {
	s := Summary{RunID: "r", Arrivals: 1000, Completed: 1000, IndexedOK: 1000}
	v := Verdict{RunID: "r", Verdict: "INCONCLUSIVE", Reason: "missing, dropped, or unreconciled generator evidence", Reconciliation: map[string]any{"generator": s, "searchable_now": 999}}
	if !RetryEvidence(v).Allowed {
		t.Fatal("recoverable indexing visibility delay rejected")
	}
	s.Dropped = 1
	v.Reconciliation = map[string]any{"generator": s, "searchable_now": 999}
	if RetryEvidence(v).Allowed {
		t.Fatal("dropped traffic disguised as ingestion lag")
	}
}

func freshStage(t *testing.T) (Frozen, Verdict, Frozen, time.Time) {
	prior, v, _ := savedTrial(t, "demo-healthy-20260918-01")
	var next Frozen
	b, _ := json.Marshal(prior)
	json.Unmarshal(b, &next)
	next.RunID = "unit-only-stage-25"
	next.Route.Metadata.RV = v.Receipt.After
	i, _ := next.Route.Rule()
	var rule map[string]any
	json.Unmarshal(next.Route.Spec.Rules[i], &rule)
	backends := rule["backendRefs"].([]any)
	backends[0].(map[string]any)["weight"] = 10
	backends[1].(map[string]any)["weight"] = 90
	next.Route.Spec.Rules[i], _ = json.Marshal(rule)
	verified, _ := time.Parse(time.RFC3339Nano, v.At)
	next.FrozenAt = Stamp(verified.Add(time.Second))
	return prior, v, next, verified.Add(2 * time.Second)
}

func TestAdvanceRequiresFreshAdjacentUnchangedStage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Frozen, *Verdict, *Frozen, *time.Time)
		allowed bool
	}{
		{"fresh", func(*Frozen, *Verdict, *Frozen, *time.Time) {}, true},
		{"failed", func(_ *Frozen, v *Verdict, _ *Frozen, _ *time.Time) { v.Verdict = "FAILED" }, false},
		{"inconclusive", func(_ *Frozen, v *Verdict, _ *Frozen, _ *time.Time) { v.Verdict = "INCONCLUSIVE" }, false},
		{"threshold equality", func(_ *Frozen, v *Verdict, _ *Frozen, _ *time.Time) { v.Windows[0].P95 = 300 }, false},
		{"stale demo pass", func(_ *Frozen, _ *Verdict, _ *Frozen, n *time.Time) { *n = n.Add(time.Hour) }, false},
		{"different config", func(_ *Frozen, _ *Verdict, n *Frozen, _ *time.Time) { n.ConfigHash = "different" }, false},
		{"different contract", func(_ *Frozen, _ *Verdict, n *Frozen, _ *time.Time) { n.Hash = "different" }, false},
		{"concurrent route change", func(_ *Frozen, _ *Verdict, n *Frozen, _ *time.Time) { n.Route.Metadata.RV = "different" }, false},
		{"recreated route", func(_ *Frozen, _ *Verdict, n *Frozen, _ *time.Time) { n.Route.Metadata.UID = "different" }, false},
		{"reused run", func(p *Frozen, _ *Verdict, n *Frozen, _ *time.Time) { n.RunID = p.RunID }, false},
		{"missing receipt", func(_ *Frozen, v *Verdict, _ *Frozen, _ *time.Time) { v.Receipt = Receipt{} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, v, n, now := freshStage(t)
			tc.mutate(&p, &v, &n, &now)
			weight, err := ValidateAdvance(p, v, n, now)
			if tc.allowed && (err != nil || weight != 25) {
				t.Fatalf("%d %v", weight, err)
			}
			if !tc.allowed && err == nil {
				t.Fatal("unsafe progression allowed")
			}
		})
	}
}
