package control

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type Contract struct {
	Version     string `json:"contract_version"`
	Service     string `json:"service"`
	Endpoint    string `json:"endpoint"`
	Observation struct {
		Count       int    `json:"window_count"`
		Seconds     int    `json:"window_seconds"`
		Overlapping bool   `json:"overlapping"`
		Anchor      string `json:"anchor"`
		Convergence int    `json:"route_convergence_allowance_seconds"`
	} `json:"observation"`
	Eligibility struct {
		Classes  []string `json:"traffic_class"`
		Excluded []string `json:"exclude_traffic_class"`
		Minimum  int      `json:"min_samples_per_window"`
	} `json:"eligibility"`
	Criteria struct {
		Success  float64 `json:"success_rate_min"`
		P95      float64 `json:"p95_latency_ms_max"`
		Method   string  `json:"percentile_method"`
		Timeout  string  `json:"timeout_counts_as"`
		Statuses []int   `json:"http_success_statuses"`
	} `json:"criteria"`
	Freshness struct {
		Lag   int  `json:"ingestion_lag_allowance_seconds"`
		Empty bool `json:"empty_result_is_never_zero_errors"`
	} `json:"freshness"`
	Verdicts struct {
		Pass    string `json:"all_windows_pass"`
		Fail    string `json:"measured_breach"`
		Missing string `json:"missing_stale_or_underpowered"`
	} `json:"verdicts"`
}

func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func LoadContract(path string) (Contract, []byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Contract{}, nil, err
	}
	c, err := ParseContract(b)
	return c, b, err
}
func ParseContract(b []byte) (Contract, error) {
	var c Contract
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Version != "1.0" || c.Service != "checkout" || c.Endpoint != "/checkout" || c.Observation.Count < 1 || c.Observation.Seconds < 1 || c.Observation.Overlapping || c.Observation.Anchor != "executor_receipt.route_effective_at" || c.Observation.Convergence < 1 || c.Eligibility.Minimum < 1 || len(c.Eligibility.Classes) != 1 || c.Eligibility.Classes[0] != "ordinary" || len(c.Eligibility.Excluded) != 2 || c.Eligibility.Excluded[0] != "synthetic" || c.Eligibility.Excluded[1] != "warmup" || c.Criteria.Success <= 0 || c.Criteria.Success > 1 || c.Criteria.P95 <= 0 || c.Criteria.Method != "tdigest" || c.Criteria.Timeout != "failure" || len(c.Criteria.Statuses) != 1 || c.Criteria.Statuses[0] != 200 || c.Freshness.Lag < 0 || !c.Freshness.Empty || c.Verdicts.Pass != "PROVED" || c.Verdicts.Fail != "FAILED" || c.Verdicts.Missing != "INCONCLUSIVE" {
		return c, fmt.Errorf("unsupported or invalid contract; refusing to reinterpret it")
	}
	return c, nil
}

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func ValidateID(s string) error {
	if !validID.MatchString(s) {
		return fmt.Errorf("invalid run ID")
	}
	return nil
}

type Frozen struct {
	Timestamp   string            `json:"@timestamp"`
	RunID       string            `json:"run_id"`
	Hash        string            `json:"contract_hash"`
	FrozenAt    string            `json:"frozen_at"`
	Actor       string            `json:"frozen_by"`
	Service     string            `json:"service"`
	Contract    json.RawMessage   `json:"contract"`
	Source      string            `json:"contract_source"`
	ConfigHash  string            `json:"candidate_config_hash"`
	QueryHashes map[string]string `json:"query_hashes"`
	Route       Route             `json:"route_snapshot"`
}

var queryNames = []string{"evidence-completeness", "window-metrics"}

func Freeze(e Elastic, run, path, queries, configHash string, route Route) (Frozen, error) {
	if err := ValidateID(run); err != nil {
		return Frozen{}, err
	}
	c, b, err := LoadContract(path)
	if err != nil {
		return Frozen{}, err
	}
	if configHash == "" {
		return Frozen{}, fmt.Errorf("live candidate config hash required")
	}
	if _, err := route.Backends(); err != nil {
		return Frozen{}, err
	}
	f := Frozen{Timestamp: Now(), RunID: run, Hash: Hash(b), Actor: "controller", Service: c.Service, Contract: b, Source: string(b), ConfigHash: configHash, Route: route, QueryHashes: map[string]string{}}
	f.FrozenAt = f.Timestamp
	for _, n := range queryNames {
		q, err := os.ReadFile(filepath.Join(queries, n+".esql"))
		if err != nil {
			return f, err
		}
		f.QueryHashes[n] = Hash(q)
	}
	err = e.Do("PUT", "/sloproof-contracts/_create/"+run+"?refresh=wait_for", f, nil)
	return f, err
}
func ReadFrozen(e Elastic, run string) (Frozen, error) {
	var d struct {
		Source Frozen `json:"_source"`
	}
	if err := ValidateID(run); err != nil {
		return d.Source, err
	}
	err := e.Do("GET", "/sloproof-contracts/_doc/"+run, nil, &d)
	if err == nil && (d.Source.RunID != run || Hash([]byte(d.Source.Source)) != d.Source.Hash) {
		err = fmt.Errorf("frozen contract hash or run mismatch")
	}
	return d.Source, err
}
func (f Frozen) ValidateLocal(path, queries string) (Contract, map[string]string, error) {
	c, b, err := LoadContract(path)
	if err != nil {
		return c, nil, err
	}
	if Hash(b) != f.Hash {
		return c, nil, fmt.Errorf("contract changed after freeze; trial invalid")
	}
	if _, err = time.Parse(time.RFC3339Nano, f.FrozenAt); err != nil {
		return c, nil, err
	}
	qs := map[string]string{}
	for _, n := range queryNames {
		q, err := os.ReadFile(filepath.Join(queries, n+".esql"))
		if err != nil {
			return c, nil, err
		}
		if Hash(q) != f.QueryHashes[n] {
			return c, nil, fmt.Errorf("query %s changed after freeze", n)
		}
		qs[n] = string(q)
	}
	return c, qs, nil
}
