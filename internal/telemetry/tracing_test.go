package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"go.opentelemetry.io/otel/sdk/resource"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildTracerProvider_Disabled(t *testing.T) {
	tp, shutdown, err := BuildTracerProvider(context.Background(), config.OTelTracingConfig{}, nil, newTestLogger())
	if err != nil {
		t.Fatalf("BuildTracerProvider error = %v", err)
	}
	if tp != nil || shutdown != nil {
		t.Fatalf("expected tracing disabled, got tp=%v shutdown_set=%t", tp, shutdown != nil)
	}
}

func TestBuildTracerProvider_ExportsToHTTPEndpoint(t *testing.T) {
	reqCh := make(chan *http.Request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		reqCh <- r
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.OTelTracingConfig{
		Endpoint: srv.URL,
		Headers:  map[string]string{"x-otlp-token": "secret"},
		Sampler:  "always_on",
	}
	tp, shutdown, err := BuildTracerProvider(context.Background(), cfg, resource.Empty(), newTestLogger())
	if err != nil {
		t.Fatalf("BuildTracerProvider error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if shutdown != nil {
			if err := shutdown(ctx); err != nil {
				t.Fatalf("shutdown error = %v", err)
			}
		}
	}()

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	span.End()
	if err := tp.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush error = %v", err)
	}

	select {
	case req := <-reqCh:
		if got, want := req.URL.Path, "/v1/traces"; got != want {
			t.Fatalf("unexpected path %q want %q", got, want)
		}
		if req.Header.Get("x-otlp-token") != "secret" {
			t.Fatalf("header not forwarded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive OTLP request")
	}
}

func TestBuildTracerProvider_InsecureTLS(t *testing.T) {
	reqCh := make(chan struct{}, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		reqCh <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.OTelTracingConfig{
		Endpoint: srv.URL,
		Insecure: true,
		Sampler:  "always_on",
	}
	tp, shutdown, err := BuildTracerProvider(context.Background(), cfg, nil, newTestLogger())
	if err != nil {
		t.Fatalf("BuildTracerProvider error = %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if shutdown != nil {
			_ = shutdown(ctx)
		}
	}()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "tls-span")
	span.End()
	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush error = %v", err)
	}

	select {
	case <-reqCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected OTLP request over TLS")
	}
}

func TestBuildTracerProvider_RatioSampler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tp, shutdown, err := BuildTracerProvider(context.Background(), config.OTelTracingConfig{
		Endpoint: srv.URL,
		Sampler:  "traceidratio:0",
	}, nil, newTestLogger())
	if err != nil {
		t.Fatalf("BuildTracerProvider error = %v", err)
	}
	defer func() {
		if shutdown != nil {
			_ = shutdown(context.Background())
		}
	}()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "ratio-span")
	span.End()
	if span.SpanContext().IsSampled() {
		t.Fatal("expected unsampled span with ratio 0")
	}
}

func TestSamplerFromConfig_Invalid(t *testing.T) {
	if _, err := samplerFromConfig("traceidratio:"); err == nil {
		t.Fatal("expected error for missing ratio")
	}
}

func TestSamplerFromConfig_Default(t *testing.T) {
	sampler, err := samplerFromConfig("")
	if err != nil {
		t.Fatalf("samplerFromConfig error = %v", err)
	}
	if sampler == nil {
		t.Fatal("expected sampler")
	}
}
