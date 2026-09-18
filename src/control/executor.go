package control

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"time"
)

type Route struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		UID       string `json:"uid"`
		RV        string `json:"resourceVersion"`
	} `json:"metadata"`
	Spec struct {
		Rules      []json.RawMessage `json:"rules"`
		ParentRefs []json.RawMessage `json:"parentRefs"`
	} `json:"spec"`
}
type Backend struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Port      int    `json:"port"`
	Weight    int    `json:"weight"`
}

func (r Route) Rule() (int, error) {
	if r.Metadata.Name != "checkout-route" || r.Metadata.Namespace != "checkout" || r.Metadata.UID == "" || r.Metadata.RV == "" {
		return 0, fmt.Errorf("unexpected route identity")
	}
	found := -1
	for i, raw := range r.Spec.Rules {
		var rule struct {
			Matches []struct {
				Headers []any                        `json:"headers"`
				Path    struct{ Type, Value string } `json:"path"`
			} `json:"matches"`
		}
		if err := json.Unmarshal(raw, &rule); err != nil {
			return 0, err
		}
		ordinary := true
		for _, m := range rule.Matches {
			if len(m.Headers) > 0 {
				ordinary = false
			}
		}
		if ordinary {
			if found >= 0 || len(rule.Matches) != 1 || rule.Matches[0].Path.Type != "PathPrefix" || rule.Matches[0].Path.Value != "/checkout" {
				return 0, fmt.Errorf("ambiguous ordinary route")
			}
			found = i
		}
	}
	if found < 0 {
		return 0, fmt.Errorf("ordinary route missing")
	}
	return found, nil
}
func (r Route) Backends() ([]Backend, error) {
	i, err := r.Rule()
	if err != nil {
		return nil, err
	}
	var rule struct {
		Backends []Backend `json:"backendRefs"`
	}
	if err = json.Unmarshal(r.Spec.Rules[i], &rule); err != nil {
		return nil, err
	}
	b := rule.Backends
	if len(b) != 2 {
		return nil, fmt.Errorf("expected two named backends")
	}
	sum := 0
	for n, v := range b {
		if v.Name != []string{"checkout-v1", "checkout-v2"}[n] || v.Port != 80 || (v.Namespace != "" && v.Namespace != "checkout") || v.Group != "" || (v.Kind != "" && v.Kind != "Service") || v.Weight < 0 || v.Weight > 100 {
			return nil, fmt.Errorf("unexpected backend identity or weight")
		}
		sum += v.Weight
	}
	if sum != 100 {
		return nil, fmt.Errorf("expected weights totaling 100")
	}
	return b, nil
}
func BuildPatch(expected, current Route, v1, v2 int) ([]map[string]any, error) {
	if v1 < 0 || v2 < 0 || v1+v2 != 100 {
		return nil, fmt.Errorf("invalid weights")
	}
	if _, err := expected.Backends(); err != nil {
		return nil, err
	}
	if _, err := current.Backends(); err != nil {
		return nil, err
	}
	// Kubernetes pretty-prints JSON while Elasticsearch may reserialize the
	// frozen snapshot. Compare decoded values, including every captured field,
	// instead of comparing RawMessage whitespace and object-key order.
	var expectedValue, currentValue any
	for _, item := range []struct {
		route Route
		value *any
	}{{expected, &expectedValue}, {current, &currentValue}} {
		encoded, err := json.Marshal(item.route)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, item.value); err != nil {
			return nil, err
		}
	}
	if !reflect.DeepEqual(expectedValue, currentValue) {
		return nil, fmt.Errorf("route conflict: UID, resourceVersion, backends, weights or rules changed; escalate")
	}
	i, _ := expected.Rule()
	var rule any
	_ = json.Unmarshal(expected.Spec.Rules[i], &rule)
	return []map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": expected.Metadata.UID},
		{"op": "test", "path": "/metadata/resourceVersion", "value": expected.Metadata.RV},
		{"op": "test", "path": fmt.Sprintf("/spec/rules/%d", i), "value": rule},
		{"op": "replace", "path": fmt.Sprintf("/spec/rules/%d/backendRefs/0/weight", i), "value": v1},
		{"op": "replace", "path": fmt.Sprintf("/spec/rules/%d/backendRefs/1/weight", i), "value": v2}}, nil
}

// Only a dedicated token-based, RBAC-limited kubeconfig is accepted. The
// executor never loads the laptop's ambient config or invokes arbitrary shell.
func kubectl(args ...string) ([]byte, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg := filepath.Join(root, ".kube/executor.config")
	if _, err = os.Stat(cfg); err != nil {
		return nil, fmt.Errorf("dedicated executor kubeconfig required: %w", err)
	}
	base := []string{"--kubeconfig", cfg, "--context", "sloproof-executor", "--request-timeout=30s", "-n", "checkout"}
	cmd := exec.Command("kubectl", append(base, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "KUBECONFIG=" + cfg}
	b, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("executor kubectl: %w: %.400s", err, b)
	}
	return b, nil
}
func GetRoute() (Route, error) {
	var r Route
	b, err := kubectl("get", "httproute", "checkout-route", "-o", "json")
	if err == nil {
		err = json.Unmarshal(b, &r)
	}
	return r, err
}

type Receipt struct {
	RunID        string `json:"run_id"`
	ContractHash string `json:"contract_hash"`
	UID          string `json:"route_uid"`
	Before       string `json:"resource_version_before"`
	After        string `json:"resource_version_after"`
	Weights      []int  `json:"weights_after"`
	Accepted     string `json:"api_accepted_at"`
	Effective    string `json:"route_effective_at"`
	Evidence     any    `json:"effective_evidence,omitempty"`
}

func Execute(e Elastic, f Frozen, v1, v2 int) (Receipt, error) {
	r := Receipt{RunID: f.RunID, ContractHash: f.Hash, UID: f.Route.Metadata.UID, Before: f.Route.Metadata.RV, Weights: []int{v1, v2}}
	current, err := GetRoute()
	if err != nil {
		return r, err
	}
	patch, err := BuildPatch(f.Route, current, v1, v2)
	if err != nil {
		le := Append(e, f.RunID, "route_conflict", f.Hash, map[string]any{"error": err.Error(), "state": "ESCALATED"}, map[string]any{"route_uid": f.Route.Metadata.UID})
		if le != nil {
			return r, le
		}
		return r, err
	}
	// Persist intent BEFORE mutation. A second execution for a run must fail.
	if err = e.Do("PUT", "/sloproof-ledger/_create/"+f.RunID+".intent?refresh=wait_for", map[string]any{"@timestamp": Now(), "run_id": f.RunID, "event": "route_intent", "contract_hash": f.Hash, "detail": patch}, nil); err != nil {
		return r, err
	}
	b, _ := json.Marshal(patch)
	after, err := kubectl("patch", "httproute", "checkout-route", "--type=json", "-p", string(b), "-o", "json")
	if err != nil {
		_ = Append(e, f.RunID, "route_conflict", f.Hash, map[string]any{"error": err.Error(), "state": "ESCALATED"}, nil)
		return r, err
	}
	r.Accepted = Now()
	var a Route
	if err = json.Unmarshal(after, &a); err != nil {
		return r, err
	}
	r.After = a.Metadata.RV
	err = WriteReceipt(e, r, false)
	return r, err
}
func WriteReceipt(e Elastic, r Receipt, effective bool) error {
	suffix := ".route"
	event := "route_weights_changed"
	if effective {
		suffix = ".effective"
		event = "route_effective"
	}
	return e.Do("PUT", "/sloproof-ledger/_create/"+r.RunID+suffix+"?refresh=wait_for", map[string]any{"@timestamp": Now(), "run_id": r.RunID, "event": event, "contract_hash": r.ContractHash, "route_uid": r.UID, "detail": r}, nil)
}
func ReadReceipt(e Elastic, run string, effective bool) (Receipt, error) {
	suffix := ".route"
	if effective {
		suffix = ".effective"
	}
	var d struct {
		Source struct {
			Detail Receipt `json:"detail"`
		} `json:"_source"`
	}
	err := e.Do("GET", "/sloproof-ledger/_doc/"+run+suffix, nil, &d)
	return d.Source.Detail, err
}
func ValidateReceipt(f Frozen, r Receipt) error {
	if r.RunID != f.RunID || r.ContractHash != f.Hash || r.UID != f.Route.Metadata.UID || r.Before != f.Route.Metadata.RV || r.After == "" || len(r.Weights) != 2 || r.Weights[0] <= 0 || r.Weights[1] < 0 || r.Weights[0]+r.Weights[1] != 100 {
		return fmt.Errorf("invalid trial receipt")
	}
	frozen, err := time.Parse(time.RFC3339Nano, f.FrozenAt)
	if err != nil {
		return err
	}
	accepted, err := time.Parse(time.RFC3339Nano, r.Accepted)
	if err != nil || accepted.Before(frozen) {
		return fmt.Errorf("routing did not follow freeze")
	}
	return nil
}
