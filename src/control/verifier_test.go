package control

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (Contract, time.Time, time.Time, []map[string]any, []map[string]any, []map[string]any) {
	t.Helper()
	c, _, err := LoadContract("../../contracts/checkout-slo.json")
	if err != nil {
		t.Fatal(err)
	}
	a := time.Date(2026, 9, 18, 7, 0, 0, 0, time.UTC)
	b := a.Add(time.Minute)
	complete := []map[string]any{{"version": "v1", "requests": float64(600), "config_hashes": float64(1), "first_seen": Stamp(a), "last_seen": Stamp(b.Add(-time.Millisecond))}}
	metrics := []map[string]any{{"version": "v1", "requests": float64(600), "successes": float64(600), "success_rate": float64(1), "p95_ms": float64(57)}}
	integrity := []map[string]any{{"invalid": float64(0), "wrong_config": float64(0), "unknown_version": float64(0)}}
	return c, a, b, complete, metrics, integrity
}
func TestWindowVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		change     func([]map[string]any, []map[string]any, []map[string]any)
	}{
		{"healthy", "PROVED", func(a, b, c []map[string]any) {}},
		{"slow", "FAILED", func(a, b, c []map[string]any) { b[0]["p95_ms"] = 501.0 }},
		{"exact threshold is breach", "FAILED", func(a, b, c []map[string]any) { b[0]["p95_ms"] = 300.0 }},
		{"errors", "FAILED", func(a, b, c []map[string]any) { b[0]["successes"] = 480.0; b[0]["success_rate"] = 0.8 }},
		{"underpowered", "INCONCLUSIVE", func(a, b, c []map[string]any) { a[0]["requests"] = 399.0 }},
		{"mismatched counts", "INCONCLUSIVE", func(a, b, c []map[string]any) { b[0]["requests"] = 601.0 }},
		{"config change", "INCONCLUSIVE", func(a, b, c []map[string]any) { c[0]["wrong_config"] = 1.0 }},
		{"unknown gateway failure", "INCONCLUSIVE", func(a, b, c []map[string]any) { c[0]["unknown_version"] = 1.0 }},
		{"missing metric", "INCONCLUSIVE", func(a, b, c []map[string]any) { delete(b[0], "p95_ms") }},
		{"truncated observations", "INCONCLUSIVE", func(a, b, c []map[string]any) { a[0]["last_seen"] = a[0]["first_seen"] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, a, b, x, y, z := fixture(t)
			tc.change(x, y, z)
			got, reason, _, _, _ := EvaluateWindow(c, a, b, x, y, z)
			if got != tc.want {
				t.Fatalf("%s: %s", got, reason)
			}
		})
	}
}
func TestEmptyNeverPasses(t *testing.T) {
	c, a, b, x, y, z := fixture(t)
	for _, v := range []struct{ x, y, z []map[string]any }{{nil, y, z}, {x, nil, z}, {x, y, nil}} {
		got, _, _, _, _ := EvaluateWindow(c, a, b, v.x, v.y, v.z)
		if got != "INCONCLUSIVE" {
			t.Fatal(got)
		}
	}
}
func TestSummaryReconciliation(t *testing.T) {
	s := Summary{RunID: "r", Arrivals: 1000, Completed: 1000, IndexedOK: 1000}
	if err := s.Validate("r", 1000); err != nil {
		t.Fatal(err)
	}
	s.Dropped = 1
	if s.Validate("r", 1000) == nil {
		t.Fatal("dropped arrival accepted")
	}
	s.Dropped = 0
	if s.Validate("r", 999) == nil {
		t.Fatal("missing document accepted")
	}
}
func TestContractAndQueryMutation(t *testing.T) {
	_, b, err := LoadContract("../../contracts/checkout-slo.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := dir + "/contract.json"
	os.WriteFile(file, append(b, ' '), 0600)
	f := Frozen{Hash: Hash(b)}
	if _, _, err = f.ValidateLocal(file, dir); err == nil {
		t.Fatal("changed contract accepted")
	}
}
func TestConvergenceRequiresObservedTraffic(t *testing.T) {
	if _, err := FindEffective(nil, .1); err == nil {
		t.Fatal("empty accepted")
	}
	rows := []map[string]any{}
	start := time.Date(2026, 9, 18, 7, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		s := Stamp(start.Add(time.Duration(i) * time.Second))
		for _, v := range []string{"v1", "v2"} {
			n := 90.0
			if v == "v1" {
				n = 10
			}
			rows = append(rows, map[string]any{"second": s, "last_seen": s, "version": v, "requests": n})
		}
	}
	at, err := FindEffective(rows, .1)
	if err != nil || !at.Equal(start.Add(2*time.Second)) {
		t.Fatalf("%v %v", at, err)
	}
	if _, err = FindEffective(rows, 1); err == nil {
		t.Fatal("wrong split accepted")
	}
}
func TestConditionalPatch(t *testing.T) {
	raw := `{"metadata":{"name":"checkout-route","namespace":"checkout","uid":"u","resourceVersion":"17"},"spec":{"rules":[{"matches":[{"path":{"type":"PathPrefix","value":"/checkout"}}],"backendRefs":[{"name":"checkout-v1","port":80,"weight":0},{"name":"checkout-v2","port":80,"weight":100}]}]}}`
	var r Route
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	// A frozen snapshot has crossed a JSON storage boundary before execution.
	// Its raw rule bytes need not match kubectl's formatting or key order.
	var pretty Route
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &pretty); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPatch(r, pretty, 10, 90); err != nil {
		t.Fatalf("equivalent JSON formatting rejected: %v", err)
	}
	var reordered map[string]any
	if err := json.Unmarshal(encoded, &reordered); err != nil {
		t.Fatal(err)
	}
	encoded, _ = json.Marshal(reordered)
	if err := json.Unmarshal(encoded, &pretty); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPatch(r, pretty, 10, 90); err != nil {
		t.Fatalf("equivalent JSON key order rejected: %v", err)
	}
	changedRule := strings.Replace(string(pretty.Spec.Rules[0]), `"weight":100`, `"weight":90`, 1)
	changedRule = strings.Replace(changedRule, `"weight":0`, `"weight":10`, 1)
	pretty.Spec.Rules[0] = json.RawMessage(changedRule)
	if _, err := BuildPatch(r, pretty, 10, 90); err == nil {
		t.Fatal("changed routing accepted after normalization")
	}
	p, err := BuildPatch(r, r, 10, 90)
	if err != nil {
		t.Fatal(err)
	}
	mutations := 0
	for _, op := range p {
		if op["op"] != "test" {
			mutations++
			if !strings.HasSuffix(op["path"].(string), "/weight") {
				t.Fatal("non-weight mutation")
			}
		}
	}
	if mutations != 2 {
		t.Fatal(mutations)
	}
	changed := r
	changed.Metadata.RV = "18"
	if _, err = BuildPatch(r, changed, 10, 90); err == nil {
		t.Fatal("concurrent edit accepted")
	}
	changed = r
	changed.Metadata.UID = "other"
	if _, err = BuildPatch(r, changed, 10, 90); err == nil {
		t.Fatal("recreated route accepted")
	}
	if _, err = BuildPatch(r, r, 100, 100); err == nil {
		t.Fatal("invalid split accepted")
	}
}
