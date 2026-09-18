package control

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type Summary struct {
	RunID         string `json:"run_id"`
	Arrivals      int64  `json:"arrivals"`
	Dropped       int64  `json:"dropped_arrivals"`
	Completed     int64  `json:"completed"`
	IndexedOK     int64  `json:"indexed_ok"`
	IndexedFailed int64  `json:"indexed_failed"`
	Searchable    int64  `json:"searchable_in_es"`
}

func (s Summary) Validate(run string, count int64) error {
	if s.RunID != run || s.Completed <= 0 || s.Arrivals != s.Completed || s.Dropped != 0 || s.IndexedFailed != 0 || s.IndexedOK != s.Completed || count != s.Completed {
		return fmt.Errorf("missing, dropped, or unreconciled generator evidence")
	}
	return nil
}

type AuditQuery struct {
	Name   string           `json:"name"`
	Query  string           `json:"query"`
	Params []map[string]any `json:"params"`
	Result QueryResult      `json:"result"`
	Error  string           `json:"error,omitempty"`
}
type Window struct {
	Index    int          `json:"window_index"`
	Start    string       `json:"window_start"`
	End      string       `json:"window_end"`
	Verdict  string       `json:"verdict"`
	Reason   string       `json:"reason"`
	Requests float64      `json:"requests"`
	Success  float64      `json:"success_rate"`
	P95      float64      `json:"p95_ms"`
	Queries  []AuditQuery `json:"queries"`
}
type Verdict struct {
	RunID          string             `json:"run_id"`
	Hash           string             `json:"contract_hash"`
	Version        string             `json:"version"`
	Verdict        string             `json:"verdict"`
	Reason         string             `json:"reason"`
	At             string             `json:"verified_at"`
	Receipt        Receipt            `json:"receipt"`
	Reconciliation any                `json:"reconciliation"`
	Windows        []Window           `json:"windows"`
	Assessment     RecoveryAssessment `json:"recovery_assessment"`
}

const integrityQuery = `FROM sloproof-requests
| WHERE run_id == ?run_id AND traffic_class == "ordinary"
    AND @timestamp >= ?window_start AND @timestamp < ?window_end
| STATS requests = COUNT(*),
        invalid = SUM(CASE(service != "checkout" OR service IS NULL OR duration_ms IS NULL OR duration_ms < 0 OR success IS NULL OR status IS NULL OR success != (status == 200), 1, 0)),
        wrong_config = SUM(CASE(version == "v1" AND (config_hash IS NULL OR config_hash != ?config_hash), 1, 0)),
        unknown_version = SUM(CASE(version IS NULL OR (version != "v1" AND version != "v2"), 1, 0))`

// VerifierScope checks actual privileges, not the environment variable's name.
// It refuses write access to any evidence or control index, and requires reads.
func VerifierScope(e Elastic) error {
	privs := []string{"read", "index", "create", "create_doc", "write", "delete", "manage", "create_index"}
	var r struct {
		Index   map[string]map[string]bool `json:"index"`
		Cluster map[string]bool            `json:"cluster"`
	}
	body := map[string]any{"cluster": []string{"manage_security", "manage_api_key"}, "index": []any{map[string]any{"names": []string{"sloproof-requests", "sloproof-contracts", "sloproof-ledger"}, "privileges": privs}}}
	if err := e.Do("POST", "/_security/user/_has_privileges", body, &r); err != nil {
		return err
	}
	for _, name := range []string{"sloproof-requests", "sloproof-contracts", "sloproof-ledger"} {
		p := r.Index[name]
		if !p["read"] {
			return fmt.Errorf("verifier cannot read %s", name)
		}
		for _, v := range privs[1:] {
			if p[v] {
				return fmt.Errorf("verifier has forbidden %s on %s", v, name)
			}
		}
	}
	for p, has := range r.Cluster {
		if has {
			return fmt.Errorf("verifier has forbidden cluster privilege %s", p)
		}
	}
	return nil
}
func Verify(e Elastic, run, path, queries string, summary Summary) (result Verdict, resultErr error) {
	defer func() { result.Assessment = Assess(result) }()
	v := Verdict{RunID: run, Version: "v1", Verdict: "INCONCLUSIVE", At: Now()}
	fail := func(err error) (Verdict, error) { v.Reason = err.Error(); return v, nil }
	if err := VerifierScope(e); err != nil {
		return fail(err)
	}
	f, err := ReadFrozen(e, run)
	if err != nil {
		return fail(err)
	}
	v.Hash = f.Hash
	c, qs, err := f.ValidateLocal(path, queries)
	if err != nil {
		return fail(err)
	}
	r, err := ReadReceipt(e, run, true)
	if err != nil {
		return fail(err)
	}
	v.Receipt = r
	if err = ValidateReceipt(f, r); err != nil {
		return fail(err)
	}
	// Re-derive the anchor from ES; a supplied receipt timestamp is not evidence.
	derived, err := Effective(e, f, r)
	if err != nil {
		return fail(err)
	}
	if derived.Effective != r.Effective {
		return fail(fmt.Errorf("receipt anchor differs from observed traffic"))
	}
	anchor, err := time.Parse(time.RFC3339Nano, r.Effective)
	if err != nil {
		return fail(err)
	}
	end := anchor.Add(time.Duration(c.Observation.Count*c.Observation.Seconds) * time.Second)
	if time.Now().Before(end.Add(time.Duration(c.Freshness.Lag) * time.Second)) {
		return fail(fmt.Errorf("window or ingestion allowance not closed"))
	}
	var count struct {
		Count  int64 `json:"count"`
		Shards struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
	}
	if err = e.Do("POST", "/sloproof-requests/_count", map[string]any{"query": map[string]any{"term": map[string]any{"run_id": run}}}, &count); err != nil {
		return fail(err)
	}
	v.Reconciliation = map[string]any{"generator": summary, "searchable_now": count.Count}
	if count.Shards.Failed > 0 {
		return fail(fmt.Errorf("partial reconciliation count"))
	}
	if err = summary.Validate(run, count.Count); err != nil {
		return fail(err)
	}
	for i := 0; i < c.Observation.Count; i++ {
		start := anchor.Add(time.Duration(i*c.Observation.Seconds) * time.Second)
		stop := start.Add(time.Duration(c.Observation.Seconds) * time.Second)
		w := Window{Index: i, Start: Stamp(start), End: Stamp(stop), Verdict: "INCONCLUSIVE"}
		params := []map[string]any{{"run_id": run}, {"window_start": w.Start}, {"window_end": w.End}}
		runQuery := func(name, q string, p []map[string]any) (QueryResult, error) {
			a := AuditQuery{Name: name, Query: q, Params: p}
			a.Result, err = e.Query(q, p)
			if err != nil {
				a.Error = err.Error()
			}
			w.Queries = append(w.Queries, a)
			return a.Result, err
		}
		complete, err := runQuery("evidence-completeness", qs["evidence-completeness"], params)
		if err != nil {
			w.Reason = err.Error()
			v.Windows = append(v.Windows, w)
			continue
		}
		// Always query completeness first, including its empty-result guard.
		if len(complete.Values) == 0 {
			w.Reason = "empty completeness result is missing evidence"
			v.Windows = append(v.Windows, w)
			continue
		}
		metrics, err := runQuery("window-metrics", qs["window-metrics"], params)
		if err != nil {
			w.Reason = err.Error()
			v.Windows = append(v.Windows, w)
			continue
		}
		ip := append(append([]map[string]any{}, params...), map[string]any{"config_hash": f.ConfigHash})
		integrity, err := runQuery("evidence-integrity", integrityQuery, ip)
		if err != nil {
			w.Reason = err.Error()
			v.Windows = append(v.Windows, w)
			continue
		}
		w.Verdict, w.Reason, w.Requests, w.Success, w.P95 = EvaluateWindow(c, start, stop, complete.Rows(), metrics.Rows(), integrity.Rows())
		v.Windows = append(v.Windows, w)
	}
	v.Verdict = "PROVED"
	v.Reason = "all frozen observation windows pass"
	for _, w := range v.Windows {
		if w.Verdict == "INCONCLUSIVE" {
			v.Verdict = "INCONCLUSIVE"
			v.Reason = "missing, stale, changed or underpowered evidence"
			break
		}
		if w.Verdict == "FAILED" {
			v.Verdict = "FAILED"
			v.Reason = "measured SLO breach"
		}
	}
	return v, nil
}
func EvaluateWindow(c Contract, start, end time.Time, complete, metrics, integrity []map[string]any) (string, string, float64, float64, float64) {
	incon := func(s string) (string, string, float64, float64, float64) { return "INCONCLUSIVE", s, 0, 0, 0 }
	if len(integrity) != 1 {
		return incon("missing integrity evidence")
	}
	for _, k := range []string{"invalid", "wrong_config", "unknown_version"} {
		n, ok := Number(integrity[0], k)
		if !ok || n != 0 {
			return incon("invalid evidence: " + k)
		}
	}
	var a, b map[string]any
	for _, row := range complete {
		if Text(row, "version") == "v1" {
			if a != nil {
				return incon("multiple candidate completeness rows")
			}
			a = row
		}
	}
	for _, row := range metrics {
		if Text(row, "version") == "v1" {
			if b != nil {
				return incon("multiple candidate metric rows")
			}
			b = row
		}
	}
	if a == nil || b == nil {
		return incon("empty candidate result is missing evidence")
	}
	n, ok := Number(a, "requests")
	if !ok || n < float64(c.Eligibility.Minimum) {
		return incon("underpowered window")
	}
	configs, ok := Number(a, "config_hashes")
	if !ok || configs != 1 {
		return incon("candidate config changed or missing")
	}
	first, err := time.Parse(time.RFC3339Nano, Text(a, "first_seen"))
	if err != nil {
		return incon("missing first timestamp")
	}
	last, err := time.Parse(time.RFC3339Nano, Text(a, "last_seen"))
	if err != nil {
		return incon("missing last timestamp")
	}
	// Use the frozen ingestion allowance as the permitted edge gap; an early
	// burst alone cannot prove a later window for which observations are absent.
	lag := time.Duration(c.Freshness.Lag) * time.Second
	if first.Before(start) || !last.Before(end) || first.After(start.Add(lag)) || last.Before(end.Add(-lag)) {
		return incon("stale or truncated window coverage")
	}
	m, ok := Number(b, "requests")
	if !ok || m != n {
		return incon("completeness and metric counts differ")
	}
	rate, ok := Number(b, "success_rate")
	p95, pok := Number(b, "p95_ms")
	successes, sok := Number(b, "successes")
	if !ok || !pok || !sok || math.IsNaN(rate) || math.IsNaN(p95) || math.IsInf(rate, 0) || math.IsInf(p95, 0) || rate < 0 || rate > 1 || p95 < 0 || math.Abs(rate-successes/n) > 1e-9 {
		return incon("missing or invalid metrics")
	}
	breaches := []string{}
	if rate < c.Criteria.Success {
		breaches = append(breaches, "success rate below minimum")
	}
	if p95 >= c.Criteria.P95 {
		breaches = append(breaches, "p95 must be strictly below maximum")
	}
	if len(breaches) > 0 {
		return "FAILED", strings.Join(breaches, "; "), n, rate, p95
	}
	return "PROVED", "window criteria pass", n, rate, p95
}
