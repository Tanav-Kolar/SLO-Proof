package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Elastic struct{ Endpoint, Key string }

func (e Elastic) Do(method, path string, body any, out any) error {
	if e.Key == "" {
		return fmt.Errorf("scoped Elasticsearch credential required; no setup-key fallback")
	}
	u, err := url.Parse(e.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("Elasticsearch endpoint must be HTTPS")
	}
	var raw []byte
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, strings.TrimRight(e.Endpoint, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "ApiKey "+e.Key)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Elasticsearch %s %s: HTTP %d: %.500s", method, path, resp.StatusCode, b)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

type QueryResult struct {
	Columns []struct {
		Name string `json:"name"`
	} `json:"columns"`
	Values    [][]any `json:"values"`
	IsPartial bool    `json:"is_partial"`
}

func (r QueryResult) Rows() []map[string]any {
	out := make([]map[string]any, 0, len(r.Values))
	for _, row := range r.Values {
		m := map[string]any{}
		for i, c := range r.Columns {
			if i < len(row) {
				m[c.Name] = row[i]
			}
		}
		out = append(out, m)
	}
	return out
}
func (e Elastic) Query(q string, params []map[string]any) (QueryResult, error) {
	var r QueryResult
	err := e.Do("POST", "/_query", map[string]any{"query": q, "params": params}, &r)
	if err == nil && r.IsPartial {
		err = fmt.Errorf("partial ES|QL response")
	}
	return r, err
}
func Number(m map[string]any, k string) (float64, bool) { v, ok := m[k].(float64); return v, ok }
func Text(m map[string]any, k string) string            { v, _ := m[k].(string); return v }
func Stamp(t time.Time) string                          { return t.UTC().Format(time.RFC3339Nano) }
func Now() string                                       { return Stamp(time.Now()) }
