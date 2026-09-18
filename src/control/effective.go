package control

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Convergence is a measured three-second cohort: >=40 observed arrivals per
// second, no unknown version, and pooled v1 share within five percentage points
// of the requested weights. This is a disclosed demo convergence heuristic,
// not a guarantee about future routing. Anchor is the last observed request,
// after that cohort, never the time the API accepted a patch.
const effectiveQuery = `FROM sloproof-requests
| WHERE run_id == ?run_id AND traffic_class == "ordinary"
    AND @timestamp >= ?window_start AND @timestamp < ?window_end
| EVAL second = DATE_TRUNC(1 second, @timestamp)
| STATS requests = COUNT(*), last_seen = MAX(@timestamp) BY second, version
| SORT second, version
| LIMIT 1000`

func Effective(e Elastic, f Frozen, r Receipt) (Receipt, error) {
	if err := ValidateReceipt(f, r); err != nil {
		return r, err
	}
	c, err := ParseContract([]byte(f.Source))
	if err != nil {
		return r, err
	}
	a, _ := time.Parse(time.RFC3339Nano, r.Accepted)
	end := a.Add(time.Duration(c.Observation.Convergence) * time.Second)
	p := []map[string]any{{"run_id": r.RunID}, {"window_start": Stamp(a)}, {"window_end": Stamp(end)}}
	q, err := e.Query(effectiveQuery, p)
	if err != nil {
		return r, err
	}
	anchor, err := FindEffective(q.Rows(), float64(r.Weights[0])/100)
	if err != nil {
		return r, err
	}
	r.Effective = Stamp(anchor)
	r.Evidence = map[string]any{"query": effectiveQuery, "params": p, "result": q, "method": "three consecutive seconds, >=40 requests/second, pooled share +/-0.05"}
	return r, nil
}
func FindEffective(rows []map[string]any, want float64) (time.Time, error) {
	type bucket struct {
		at, last  time.Time
		total, v1 float64
		unknown   bool
	}
	buckets := map[string]*bucket{}
	for _, row := range rows {
		s := Text(row, "second")
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return time.Time{}, err
		}
		last, err := time.Parse(time.RFC3339Nano, Text(row, "last_seen"))
		if err != nil {
			return time.Time{}, err
		}
		n, ok := Number(row, "requests")
		if !ok || n <= 0 {
			return time.Time{}, fmt.Errorf("invalid convergence count")
		}
		b := buckets[s]
		if b == nil {
			b = &bucket{at: t}
			buckets[s] = b
		}
		b.total += n
		if last.After(b.last) {
			b.last = last
		}
		switch Text(row, "version") {
		case "v1":
			b.v1 += n
		case "v2":
		default:
			b.unknown = true
		}
	}
	bs := []*bucket{}
	for _, b := range buckets {
		bs = append(bs, b)
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].at.Before(bs[j].at) })
	for i := 2; i < len(bs); i++ {
		n, v := 0.0, 0.0
		ok := true
		for j := i - 2; j <= i; j++ {
			b := bs[j]
			if b.total < 40 || b.unknown || (j > i-2 && b.at.Sub(bs[j-1].at) != time.Second) {
				ok = false
			}
			n += b.total
			v += b.v1
		}
		if ok && math.Abs(v/n-want) <= 0.05 {
			return bs[i].last, nil
		}
	}
	return time.Time{}, fmt.Errorf("no measured convergence cohort; INCONCLUSIVE")
}
