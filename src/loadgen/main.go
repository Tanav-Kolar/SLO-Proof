// Command loadgen drives the checkout service at a constant arrival rate and
// ships every client-observed outcome to Elasticsearch.
//
// Client-observed, not service-observed, on purpose: this is the only vantage
// point that sees timeouts, connection failures, and gateway errors for requests
// that never reached a pod. Service-side logs cannot record a request they never
// received, so using them to compute success rate would silently flatter the
// result exactly when the service is worst.
//
// Open model: arrivals are scheduled by a ticker and dispatched to a bounded
// worker pool. When the pool is saturated the arrival is counted as DROPPED
// rather than delayed. A closed-loop generator would quietly reduce its own
// rate as latency rose, so an 800 ms regression would show up as less traffic
// instead of as slow traffic, and the measured window would be undersized
// without anyone noticing.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type config struct {
	targetURL    string
	rps          int
	duration     time.Duration
	warmup       time.Duration
	runID        string
	trafficClass string
	generatorID  string
	maxInflight  int
	reqTimeout   time.Duration

	esEndpoint string
	esAPIKey   string
	esIndex    string
	batchSize  int
	flushEvery time.Duration
	dryRun     bool
}

// record mirrors the sloproof-requests mapping exactly. That mapping is
// dynamic:strict, so an unexpected field here fails the bulk item loudly
// instead of silently reshaping the verifier's schema.
type record struct {
	Timestamp    string  `json:"@timestamp"`
	RunID        string  `json:"run_id"`
	RequestID    string  `json:"request_id"`
	TrafficClass string  `json:"traffic_class"`
	Service      string  `json:"service"`
	Version      string  `json:"version"`
	Status       int     `json:"status"`
	Outcome      string  `json:"outcome"`
	Success      bool    `json:"success"`
	DurationMS   float64 `json:"duration_ms"`
	StartedAt    string  `json:"started_at"`
	EndedAt      string  `json:"ended_at"`
	GeneratorID  string  `json:"generator_id"`
	ConfigHash   string  `json:"config_hash"`
	TargetHost   string  `json:"target_host"`
}

type stats struct {
	arrivals    atomic.Uint64 // ticks: what the open model intended to send
	dropped     atomic.Uint64 // arrivals refused because the pool was saturated
	completed   atomic.Uint64 // requests that produced a record
	indexedOK   atomic.Uint64
	indexedFail atomic.Uint64
	bulkCalls   atomic.Uint64
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.targetURL, "url", env("TARGET_URL", "http://127.0.0.1:8080/checkout"), "checkout endpoint through the gateway")
	flag.IntVar(&cfg.rps, "rps", envInt("RPS", 100), "target arrival rate")
	flag.DurationVar(&cfg.duration, "duration", envDur("DURATION", 60*time.Second), "0 runs until interrupted")
	flag.DurationVar(&cfg.warmup, "warmup", envDur("WARMUP", 10*time.Second), "initial period labelled traffic_class=warmup and excluded by the contract")
	flag.StringVar(&cfg.runID, "run-id", env("RUN_ID", ""), "defaults to a timestamp")
	flag.StringVar(&cfg.trafficClass, "traffic-class", env("TRAFFIC_CLASS", "ordinary"), "ordinary or synthetic")
	flag.StringVar(&cfg.generatorID, "generator-id", env("GENERATOR_ID", "loadgen-1"), "")
	flag.IntVar(&cfg.maxInflight, "max-inflight", envInt("MAX_INFLIGHT", 400), "bound on concurrent requests")
	flag.DurationVar(&cfg.reqTimeout, "request-timeout", envDur("REQUEST_TIMEOUT", 10*time.Second), "")
	flag.StringVar(&cfg.esIndex, "index", env("ES_INDEX", "sloproof-requests"), "")
	flag.IntVar(&cfg.batchSize, "batch-size", envInt("BATCH_SIZE", 500), "")
	flag.DurationVar(&cfg.flushEvery, "flush-interval", envDur("FLUSH_INTERVAL", 2*time.Second), "")
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "measure without shipping to Elasticsearch")
	flag.Parse()

	cfg.esEndpoint = os.Getenv("ELASTIC_CLOUD_ENDPOINT")
	cfg.esAPIKey = os.Getenv("ELASTIC_API_KEY")
	if cfg.runID == "" {
		cfg.runID = "run-" + time.Now().UTC().Format("20060102T150405Z")
	}
	if !cfg.dryRun && (cfg.esEndpoint == "" || cfg.esAPIKey == "") {
		log.Fatal("ELASTIC_CLOUD_ENDPOINT and ELASTIC_API_KEY must be set (or pass -dry-run)")
	}
	if cfg.rps <= 0 {
		log.Fatal("-rps must be > 0")
	}

	host := cfg.targetURL
	if u, err := url.Parse(cfg.targetURL); err == nil {
		host = u.Host
	}

	st := &stats{}
	records := make(chan record, 4096)

	// Shipping runs on its own goroutine so indexing never sits on the
	// measured request path.
	var shipWG sync.WaitGroup
	shipWG.Add(1)
	go func() { defer shipWG.Done(); ship(cfg, records, st) }()

	client := &http.Client{
		Timeout: cfg.reqTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.maxInflight * 2,
			MaxIdleConnsPerHost: cfg.maxInflight * 2,
			MaxConnsPerHost:     0,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; log.Println("interrupted; draining"); cancel() }()
	if cfg.duration > 0 {
		go func() { time.Sleep(cfg.duration); cancel() }()
	}

	start := time.Now()
	log.Printf("run_id=%s rps=%d target=%s warmup=%s duration=%s",
		cfg.runID, cfg.rps, cfg.targetURL, cfg.warmup, cfg.duration)

	sem := make(chan struct{}, cfg.maxInflight)
	ticker := time.NewTicker(time.Second / time.Duration(cfg.rps))
	defer ticker.Stop()
	var reqWG sync.WaitGroup

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			st.arrivals.Add(1)
			class := cfg.trafficClass
			if time.Since(start) < cfg.warmup {
				class = "warmup"
			}
			select {
			case sem <- struct{}{}:
				reqWG.Add(1)
				go func() {
					defer reqWG.Done()
					defer func() { <-sem }()
					rec := doRequest(client, cfg, class, host)
					st.completed.Add(1)
					records <- rec
				}()
			default:
				// Saturated. Never block the ticker: a delayed arrival would
				// silently convert the open model into a closed one.
				st.dropped.Add(1)
			}
		}
	}

	reqWG.Wait()
	close(records)
	shipWG.Wait()

	report(cfg, st, time.Since(start))
}

func doRequest(client *http.Client, cfg config, class, host string) record {
	reqID := newID()
	started := time.Now().UTC()

	rec := record{
		RunID:        cfg.runID,
		RequestID:    reqID,
		TrafficClass: class,
		Service:      "checkout",
		Version:      "unknown",
		GeneratorID:  cfg.generatorID,
		TargetHost:   host,
		StartedAt:    started.Format(time.RFC3339Nano),
		Timestamp:    started.Format(time.RFC3339Nano),
	}

	req, err := http.NewRequest(http.MethodPost, cfg.targetURL, nil)
	if err != nil {
		rec.Outcome = "build_error"
		rec.finish(started)
		return rec
	}
	req.Header.Set("X-Request-Id", reqID)
	req.Header.Set("X-Run-Id", cfg.runID)
	req.Header.Set("X-Sloproof-Traffic-Class", class)

	resp, err := client.Do(req)
	if err != nil {
		// A request that never got an answer is a failure of the service from
		// the client's point of view, and must be counted as one.
		rec.Outcome = "timeout_or_connect_error"
		rec.Status = 0
		rec.Success = false
		rec.finish(started)
		return rec
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	rec.Status = resp.StatusCode
	rec.Success = resp.StatusCode == http.StatusOK
	if v := resp.Header.Get("X-Sloproof-Version"); v != "" {
		rec.Version = v
	}
	rec.ConfigHash = resp.Header.Get("X-Sloproof-Config-Hash")
	switch {
	case rec.Success:
		rec.Outcome = "success"
	case resp.StatusCode == http.StatusGatewayTimeout:
		rec.Outcome = "timeout"
	default:
		rec.Outcome = "http_error"
	}
	rec.finish(started)
	return rec
}

func (r *record) finish(started time.Time) {
	end := time.Now().UTC()
	r.EndedAt = end.Format(time.RFC3339Nano)
	r.DurationMS = float64(end.Sub(started).Microseconds()) / 1000.0
}

// ship batches records and writes them with request_id as the document _id, so
// a retried or duplicated bulk item overwrites rather than double-counting.
func ship(cfg config, in <-chan record, st *stats) {
	batch := make([]record, 0, cfg.batchSize)
	tick := time.NewTicker(cfg.flushEvery)
	defer tick.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		bulkWrite(cfg, batch, st)
		batch = batch[:0]
	}

	for {
		select {
		case rec, ok := <-in:
			if !ok {
				flush()
				return
			}
			batch = append(batch, rec)
			if len(batch) >= cfg.batchSize {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

func bulkWrite(cfg config, batch []record, st *stats) {
	if cfg.dryRun {
		st.indexedOK.Add(uint64(len(batch)))
		return
	}

	var body bytes.Buffer
	for _, r := range batch {
		meta := map[string]map[string]string{"index": {"_id": r.RequestID}}
		m, _ := json.Marshal(meta)
		body.Write(m)
		body.WriteByte('\n')
		d, _ := json.Marshal(r)
		body.Write(d)
		body.WriteByte('\n')
	}

	req, err := http.NewRequest(http.MethodPost,
		cfg.esEndpoint+"/"+cfg.esIndex+"/_bulk", bytes.NewReader(body.Bytes()))
	if err != nil {
		st.indexedFail.Add(uint64(len(batch)))
		return
	}
	req.Header.Set("Authorization", "ApiKey "+cfg.esAPIKey)
	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	st.bulkCalls.Add(1)
	if err != nil {
		log.Printf("bulk transport error: %v", err)
		st.indexedFail.Add(uint64(len(batch)))
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		log.Printf("bulk HTTP %d: %s", resp.StatusCode, truncate(string(raw), 400))
		st.indexedFail.Add(uint64(len(batch)))
		return
	}

	// A 200 from _bulk does not mean every document was written. Item-level
	// errors have to be read, or an INCONCLUSIVE run would be scored as if the
	// evidence were complete.
	var parsed struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Status int             `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		st.indexedFail.Add(uint64(len(batch)))
		return
	}
	var ok, failed uint64
	for _, item := range parsed.Items {
		for _, v := range item {
			if v.Status >= 200 && v.Status < 300 {
				ok++
			} else {
				failed++
				if failed == 1 {
					log.Printf("bulk item error: status=%d %s", v.Status, truncate(string(v.Error), 300))
				}
			}
		}
	}
	st.indexedOK.Add(ok)
	st.indexedFail.Add(failed)
}

// report prints the run summary and reconciles what was sent against what is
// actually searchable. A gap means the evidence is incomplete, which the
// contract treats as INCONCLUSIVE rather than as a pass.
func report(cfg config, st *stats, elapsed time.Duration) {
	arrivals := st.arrivals.Load()
	dropped := st.dropped.Load()
	completed := st.completed.Load()

	searchable := int64(-1)
	if !cfg.dryRun {
		if n, err := searchableCount(cfg); err == nil {
			searchable = n
		} else {
			log.Printf("reconciliation query failed: %v", err)
		}
	}

	summary := map[string]any{
		"run_id":           cfg.runID,
		"elapsed_seconds":  round(elapsed.Seconds(), 1),
		"target_rps":       cfg.rps,
		"achieved_rps":     round(float64(completed)/elapsed.Seconds(), 1),
		"arrivals":         arrivals,
		"dropped_arrivals": dropped,
		"completed":        completed,
		"indexed_ok":       st.indexedOK.Load(),
		"indexed_failed":   st.indexedFail.Load(),
		"bulk_calls":       st.bulkCalls.Load(),
		"searchable_in_es": searchable,
		"reconciled":       searchable == int64(completed),
	}
	out, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(out))

	if dropped > 0 {
		fmt.Printf("\nWARNING: %d arrivals were dropped because the pool was saturated.\n"+
			"Windows must be sized from measured counts, never from target_rps.\n", dropped)
	}
	if searchable >= 0 && searchable != int64(completed) {
		fmt.Printf("\nWARNING: %d records completed but %d are searchable. Evidence is\n"+
			"incomplete; a verdict over this window is INCONCLUSIVE, not a pass.\n", completed, searchable)
	}
}

func searchableCount(cfg config) (int64, error) {
	// Serverless indexes are near-real-time; refresh so the count is not a
	// function of when we happened to ask. This needs the API key like any
	// other call -- an unauthenticated refresh 401s silently and the count
	// then under-reports, which looks exactly like lost evidence.
	refreshReq, _ := http.NewRequest(http.MethodPost, cfg.esEndpoint+"/"+cfg.esIndex+"/_refresh", nil)
	refreshReq.Header.Set("Authorization", "ApiKey "+cfg.esAPIKey)
	refreshResp, err := (&http.Client{Timeout: 30 * time.Second}).Do(refreshReq)
	if err != nil {
		return 0, fmt.Errorf("refresh: %w", err)
	}
	refreshResp.Body.Close()
	if refreshResp.StatusCode >= 300 {
		return 0, fmt.Errorf("refresh returned HTTP %d", refreshResp.StatusCode)
	}

	q := fmt.Sprintf(`{"query":{"term":{"run_id":%q}}}`, cfg.runID)
	req, _ := http.NewRequest(http.MethodPost, cfg.esEndpoint+"/"+cfg.esIndex+"/_count", bytes.NewReader([]byte(q)))
	req.Header.Set("Authorization", "ApiKey "+cfg.esAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var out struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func round(f float64, places int) float64 {
	shift := 1.0
	for i := 0; i < places; i++ {
		shift *= 10
	}
	return float64(int(f*shift+0.5)) / shift
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return d
}

func envDur(k string, d time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if p, err := time.ParseDuration(v); err == nil {
			return p
		}
	}
	return d
}
