package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracingDisabledWithoutEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "false")
	p, err := initTracer(context.Background(), config{ServiceName: "checkout", Version: "v1"})
	if err != nil || p != nil {
		t.Fatalf("optional tracing started without an endpoint: %v %v", p, err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://unused.invalid")
	t.Setenv("OTEL_SDK_DISABLED", "true")
	p, err = initTracer(context.Background(), config{ServiceName: "checkout", Version: "v1"})
	if err != nil || p != nil {
		t.Fatal("OTEL_SDK_DISABLED must prevent exporter startup")
	}
}

func TestDiagnosticSpansPreserveRequestOutcomeAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name          string
		every, status int
	}{{"healthy", 0, 200}, {"injected-error", 1, 500}} {
		t.Run(tc.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			old := tracer
			tracer = provider.Tracer("checkout")
			defer func() { tracer = old; _ = provider.Shutdown(context.Background()) }()
			s := server{cfg: config{ServiceName: "checkout", Version: "v1", FaultErrorEveryN: tc.every, FaultErrorStatus: 500, RequestTimeoutMS: 1000}, fingerprint: "fixed-config", log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
			req := httptest.NewRequest("POST", "/checkout", nil)
			req.Header.Set("X-Run-Id", "trace-test")
			req.Header.Set("X-Request-Id", "request-test")
			response := httptest.NewRecorder()
			s.handleCheckout(response, req)
			if response.Code != tc.status || response.Header().Get("X-Sloproof-Version") != "v1" || response.Header().Get("X-Sloproof-Config-Hash") != "fixed-config" {
				t.Fatal("tracing changed service response")
			}
			spans := exporter.GetSpans()
			if len(spans) != 2 {
				t.Fatalf("expected request and simulated child; got %d", len(spans))
			}
			attrs := map[string]string{}
			simulated := false
			for _, span := range spans {
				if tc.status == 500 && span.Status.Code != codes.Error {
					t.Fatal("fault missing from diagnostic span")
				}
				for _, a := range span.Attributes {
					attrs[string(a.Key)] = a.Value.Emit()
					if string(a.Key) == "sloproof.simulated_dependency" && a.Value.AsBool() {
						simulated = true
					}
				}
			}
			if !simulated || attrs["sloproof.run_id"] != "trace-test" || attrs["sloproof.request_id"] != "request-test" || attrs["service.version"] != "v1" {
				t.Fatal("missing trace correlation or simulation label", attrs)
			}
		})
	}
}
