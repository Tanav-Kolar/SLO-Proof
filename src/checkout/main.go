// Command checkout is the SLOProof demonstration service.
//
// One binary serves both demo versions. "checkout-v1" and "checkout-v2" differ
// only by configuration, so a recovery is a configuration rollback rather than a
// code rollback. Every response and log line carries the serving version, the
// fault profile, and a behaviour fingerprint so the evidence ledger can prove
// which behaviour was live during a measurement window.
//
// Faults are set at process start from the environment. There is deliberately no
// endpoint that enables or disables a fault: the recovery executor must not be
// able to cause the incident it is trusted to repair.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// tracer emits two spans per checkout request: the request span itself
// ("POST /checkout") and a child representing the simulated call to the
// quote dependency ("checkout -> quote"). These are optional diagnostics;
// the recovery verifier reads client-observed request records, not these spans.
var tracer = otel.Tracer("checkout")

//go:embed profiles.json
var profilesJSON []byte

type profile struct {
	Description         string `json:"description"`
	BaseLatencyMS       int    `json:"base_latency_ms"`
	BaseLatencyJitterMS int    `json:"base_latency_jitter_ms"`
	FaultLatencyMS      int    `json:"fault_latency_ms"`
	FaultErrorEveryN    int    `json:"fault_error_every_n"`
	FaultErrorStatus    int    `json:"fault_error_status"`
	TelemetryMode       string `json:"telemetry_mode"`
}

type config struct {
	ServiceName  string `json:"service_name"`
	Version      string `json:"version"`
	FaultProfile string `json:"fault_profile"`
	RunID        string `json:"run_id"`
	ImageDigest  string `json:"image_digest"`
	Port         int    `json:"port"`

	BaseLatencyMS       int    `json:"base_latency_ms"`
	BaseLatencyJitterMS int    `json:"base_latency_jitter_ms"`
	FaultLatencyMS      int    `json:"fault_latency_ms"`
	FaultErrorEveryN    int    `json:"fault_error_every_n"`
	FaultErrorStatus    int    `json:"fault_error_status"`
	TelemetryMode       string `json:"telemetry_mode"`

	RequestTimeoutMS int `json:"request_timeout_ms"`
	ReadinessDelayMS int `json:"readiness_delay_ms"`
	ShutdownGraceMS  int `json:"shutdown_grace_ms"`
}

// behaviourFingerprint hashes only the fields that change how the service
// responds. Run ID and image digest are excluded so the same behaviour keeps the
// same fingerprint across demo runs, which is what makes the value comparable
// between the contract-freeze snapshot and the verdict snapshot.
func (c config) behaviourFingerprint() string {
	payload := fmt.Sprintf("v=%s|profile=%s|base=%d|jitter=%d|faultlat=%d|faulterr=%d|faultstatus=%d|telemetry=%s|timeout=%d",
		c.Version, c.FaultProfile, c.BaseLatencyMS, c.BaseLatencyJitterMS,
		c.FaultLatencyMS, c.FaultErrorEveryN, c.FaultErrorStatus, c.TelemetryMode, c.RequestTimeoutMS)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])[:16]
}

func loadConfig() (config, error) {
	profiles := map[string]profile{}
	if err := json.Unmarshal(profilesJSON, &profiles); err != nil {
		return config{}, fmt.Errorf("parse embedded profiles.json: %w", err)
	}

	name := envStr("FAULT_PROFILE", "healthy")
	p, ok := profiles[name]
	if !ok {
		known := make([]string, 0, len(profiles))
		for k := range profiles {
			known = append(known, k)
		}
		return config{}, fmt.Errorf("unknown FAULT_PROFILE %q (known profiles: %s)", name, strings.Join(known, ", "))
	}

	cfg := config{
		ServiceName:  envStr("SERVICE_NAME", "checkout"),
		Version:      envStr("SERVICE_VERSION", "v1"),
		FaultProfile: name,
		RunID:        envStr("RUN_ID", "unset"),
		ImageDigest:  envStr("IMAGE_DIGEST", "unset"),
		Port:         envInt("PORT", 8080),

		// Profile presets, each overridable for one-off experiments.
		BaseLatencyMS:       envInt("BASE_LATENCY_MS", p.BaseLatencyMS),
		BaseLatencyJitterMS: envInt("BASE_LATENCY_JITTER_MS", p.BaseLatencyJitterMS),
		FaultLatencyMS:      envInt("FAULT_LATENCY_MS", p.FaultLatencyMS),
		FaultErrorEveryN:    envInt("FAULT_ERROR_EVERY_N", p.FaultErrorEveryN),
		FaultErrorStatus:    envInt("FAULT_ERROR_STATUS", p.FaultErrorStatus),
		TelemetryMode:       envStr("TELEMETRY_MODE", p.TelemetryMode),

		RequestTimeoutMS: envInt("REQUEST_TIMEOUT_MS", 3000),
		ReadinessDelayMS: envInt("READINESS_DELAY_MS", 2000),
		ShutdownGraceMS:  envInt("SHUTDOWN_GRACE_MS", 5000),
	}
	if cfg.TelemetryMode == "" {
		cfg.TelemetryMode = "normal"
	}
	if cfg.TelemetryMode != "normal" && cfg.TelemetryMode != "suppress" {
		return config{}, errors.New("TELEMETRY_MODE must be normal or suppress")
	}

	if cfg.FaultErrorEveryN < 0 {
		return config{}, errors.New("FAULT_ERROR_EVERY_N must be >= 0 (0 disables error injection)")
	}
	if cfg.RequestTimeoutMS <= 0 {
		return config{}, errors.New("REQUEST_TIMEOUT_MS must be > 0")
	}
	return cfg, nil
}

type server struct {
	cfg         config
	fingerprint string
	startedAt   time.Time
	counter     atomic.Uint64
	ready       atomic.Bool
	log         *slog.Logger
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := loadConfig()
	if err != nil {
		log.Error("configuration rejected", "error", err.Error())
		os.Exit(1)
	}

	s := &server{cfg: cfg, fingerprint: cfg.behaviourFingerprint(), startedAt: time.Now().UTC(), log: log}

	// Tracing is optional. No configured endpoint means no exporter or network
	// activity; an unavailable trace backend never blocks checkout startup.
	tp, err := initTracer(context.Background(), cfg)
	if err != nil {
		log.Error("otel init", "error", err.Error())
	} else if tp != nil {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = tp.Shutdown(ctx)
		}()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/checkout", s.handleCheckout)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/version", s.handleVersion)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		// Write timeout must clear the worst injected delay or the fault would
		// surface as a transport error instead of the intended slow response.
		WriteTimeout: time.Duration(cfg.RequestTimeoutMS)*time.Millisecond + 5*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Readiness lags startup so a rollout does not admit traffic before the
	// process is warm. Liveness is green immediately.
	go func() {
		time.Sleep(time.Duration(cfg.ReadinessDelayMS) * time.Millisecond)
		s.ready.Store(true)
		log.Info("ready", "version", cfg.Version, "fault_profile", cfg.FaultProfile)
	}()

	cfgJSON, _ := json.Marshal(cfg)
	log.Info("starting",
		"event", "startup",
		"config", json.RawMessage(cfgJSON),
		"config_hash", s.fingerprint,
		"profile_description", profileDescription(cfg.FaultProfile),
	)

	idle := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig

		// Fail readiness first, then keep serving for the grace period so the
		// endpoints controller removes this pod before connections stop. Without
		// this, every route weight change manufactures errors that look like
		// application failure and pollute the evidence.
		s.ready.Store(false)
		log.Info("draining", "event", "shutdown_started", "grace_ms", cfg.ShutdownGraceMS)
		time.Sleep(time.Duration(cfg.ShutdownGraceMS) * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("shutdown", "error", err.Error())
		}
		close(idle)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "error", err.Error())
		os.Exit(1)
	}
	<-idle
	log.Info("stopped", "event", "shutdown_complete", "served", s.counter.Load())
}

func (s *server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	reqCtx, span := tracer.Start(r.Context(), "POST /checkout")
	defer span.End()

	start := time.Now().UTC()
	n := s.counter.Add(1)

	reqID := headerOr(r, "X-Request-Id", newID())
	runID := headerOr(r, "X-Run-Id", s.cfg.RunID)
	class := strings.ToLower(headerOr(r, "X-Sloproof-Traffic-Class", "ordinary"))
	span.SetAttributes(
		attribute.String("sloproof.run_id", runID),
		attribute.String("sloproof.request_id", reqID),
		attribute.String("sloproof.traffic_class", class),
		attribute.String("sloproof.config_hash", s.fingerprint),
		semconv.ServiceVersion(s.cfg.Version),
	)

	// Jitter is derived from the request sequence rather than a RNG so a rerun
	// of the same run produces the same latency distribution.
	jitter := 0
	if s.cfg.BaseLatencyJitterMS > 0 {
		jitter = int(mix(n) % uint64(s.cfg.BaseLatencyJitterMS+1))
	}
	delay := time.Duration(s.cfg.BaseLatencyMS+jitter+s.cfg.FaultLatencyMS) * time.Millisecond
	injected := s.cfg.FaultErrorEveryN > 0 && n%uint64(s.cfg.FaultErrorEveryN) == 0

	ctx, cancel := context.WithTimeout(reqCtx, time.Duration(s.cfg.RequestTimeoutMS)*time.Millisecond)
	defer cancel()

	// The quote dependency call is simulated in-process (there is no separate
	// quote service in this demo). Label that explicitly: it is not independent
	// dependency evidence and does not participate in the SLO verdict.
	_, depSpan := tracer.Start(ctx, "checkout -> quote")
	depSpan.SetAttributes(attribute.Bool("sloproof.simulated_dependency", true))

	status := http.StatusOK
	outcome := "success"
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		if injected {
			status = s.cfg.FaultErrorStatus
			outcome = "injected_error"
		}
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			// Client hung up. Nothing useful to write; record it and stop.
			depSpan.SetStatus(codes.Error, "client_cancelled")
			depSpan.End()
			span.SetStatus(codes.Error, "client_cancelled")
			s.logRequest(r, start, n, reqID, runID, class, 499, "client_cancelled", injected, delay)
			return
		}
		status = http.StatusGatewayTimeout
		outcome = "timeout"
	}

	if outcome != "success" {
		depSpan.SetStatus(codes.Error, outcome)
	}
	depSpan.End()
	if status >= 500 {
		span.SetStatus(codes.Error, outcome)
	}

	duration := time.Since(start)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", reqID)
	w.Header().Set("X-Sloproof-Version", s.cfg.Version)
	w.Header().Set("X-Sloproof-Run-Id", runID)
	w.Header().Set("X-Sloproof-Config-Hash", s.fingerprint)
	w.Header().Set("X-Sloproof-Traffic-Class", class)
	w.Header().Set("X-Sloproof-Telemetry-Mode", s.cfg.TelemetryMode)
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"service":       s.cfg.ServiceName,
		"version":       s.cfg.Version,
		"request_id":    reqID,
		"run_id":        runID,
		"traffic_class": class,
		"sequence":      n,
		"outcome":       outcome,
		"duration_ms":   duration.Milliseconds(),
		"fault_profile": s.cfg.FaultProfile,
		"config_hash":   s.fingerprint,
		"observed_at":   start.Format(time.RFC3339Nano),
	})

	s.logRequest(r, start, n, reqID, runID, class, status, outcome, injected, duration)
}

// logRequest emits the service-side record. The verifier reads client-observed
// outcomes instead, because those also capture requests that never reached a
// pod; these lines exist to reconcile against that index.
func (s *server) logRequest(r *http.Request, start time.Time, n uint64, reqID, runID, class string, status int, outcome string, injected bool, d time.Duration) {
	s.log.Info("checkout",
		"event", "checkout_request",
		"service", s.cfg.ServiceName,
		"version", s.cfg.Version,
		"run_id", runID,
		"request_id", reqID,
		"traffic_class", class,
		"sequence", n,
		"method", r.Method,
		"status", status,
		"outcome", outcome,
		"fault_injected", injected,
		"duration_ms", d.Milliseconds(),
		"fault_profile", s.cfg.FaultProfile,
		"config_hash", s.fingerprint,
		"observed_at", start.Format(time.RFC3339Nano),
	)
}

// handleHealthz stays green while a fault profile is active. If liveness tracked
// the injected errors, Kubernetes would restart checkout-v2 and remove the
// incident the routing experiment is meant to measure.
func (s *server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "alive", "version": s.cfg.Version})
}

func (s *server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if !s.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "version": s.cfg.Version})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "version": s.cfg.Version})
}

// handleVersion is the provenance snapshot. Capture it for both versions at
// contract-freeze time and again at verdict time, and store both in the ledger:
// matching config hashes are what rule out a mid-trial behaviour change.
func (s *server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":             s.cfg.ServiceName,
		"version":             s.cfg.Version,
		"fault_profile":       s.cfg.FaultProfile,
		"profile_description": profileDescription(s.cfg.FaultProfile),
		"config_hash":         s.fingerprint,
		"image_digest":        s.cfg.ImageDigest,
		"run_id":              s.cfg.RunID,
		"started_at":          s.startedAt.Format(time.RFC3339Nano),
		"uptime_seconds":      int(time.Since(s.startedAt).Seconds()),
		"served":              s.counter.Load(),
		"effective_config":    s.cfg,
	})
}

// initTracer wires an OTLP/HTTP exporter that reads its destination and
// credential from the standard OTEL_EXPORTER_OTLP_ENDPOINT and
// OTEL_EXPORTER_OTLP_HEADERS environment variables (never hardcoded here).
func initTracer(ctx context.Context, cfg config) (*sdktrace.TracerProvider, error) {
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") ||
		(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "") {
		return nil, nil
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}
	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(cfg.ServiceName), semconv.ServiceVersion(cfg.Version)))
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp, nil
}

func profileDescription(name string) string {
	profiles := map[string]profile{}
	if err := json.Unmarshal(profilesJSON, &profiles); err != nil {
		return ""
	}
	return profiles[name].Description
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func headerOr(r *http.Request, key, fallback string) string {
	if v := strings.TrimSpace(r.Header.Get(key)); v != "" {
		return v
	}
	return fallback
}

func envStr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "req-unknown"
	}
	return "req-" + hex.EncodeToString(b)
}

// mix is splitmix64: a cheap, lock-free way to spread the jitter across the
// configured range while staying reproducible for a given request sequence.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
