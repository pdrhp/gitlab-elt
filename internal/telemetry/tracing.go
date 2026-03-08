package telemetry

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// BuildTracerProvider configures an OTLP/HTTP trace exporter and TracerProvider.
// When cfg.Endpoint is empty, tracing is disabled and the return values are (nil, nil, nil).
func BuildTracerProvider(ctx context.Context, cfg config.OTelTracingConfig, res *resource.Resource, logger *slog.Logger) (*sdktrace.TracerProvider, func(context.Context) error, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		if logger != nil {
			logger.Info("otel tracing disabled: no endpoint configured")
		}
		return nil, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	useEndpointURL := strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://")
	opts := []otlptracehttp.Option{}
	if useEndpointURL {
		opts = append(opts, otlptracehttp.WithEndpointURL(endpoint))
	} else {
		opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
	}
	if cfg.Insecure {
		if strings.HasPrefix(endpoint, "https://") {
			opts = append(opts, otlptracehttp.WithTLSClientConfig(&tls.Config{InsecureSkipVerify: true}))
		} else {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		if logger != nil {
			logger.Error("failed to create otlp trace exporter", "error", err)
		}
		return nil, nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	sampler, err := samplerFromConfig(cfg.Sampler)
	if err != nil {
		return nil, nil, err
	}

	providerOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(exporter),
	}
	if res != nil {
		providerOpts = append(providerOpts, sdktrace.WithResource(res))
	}

	tp := sdktrace.NewTracerProvider(providerOpts...)
	shutdown := func(shutdownCtx context.Context) error {
		return tp.Shutdown(shutdownCtx)
	}
	return tp, shutdown, nil
}

func samplerFromConfig(value string) (sdktrace.Sampler, error) {
	switch value {
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "", config.DefaultOTelTracingSampler:
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	}

	const ratioPrefix = "traceidratio:"
	if strings.HasPrefix(value, ratioPrefix) {
		ratioStr := strings.TrimSpace(strings.TrimPrefix(value, ratioPrefix))
		if ratioStr == "" {
			return nil, fmt.Errorf("invalid sampler %q: missing ratio", value)
		}
		ratio, err := strconv.ParseFloat(ratioStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid sampler %q: %w", value, err)
		}
		if ratio < 0 || ratio > 1 {
			return nil, fmt.Errorf("invalid sampler %q: ratio must be between 0 and 1", value)
		}
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), nil
	}

	return nil, fmt.Errorf("unsupported sampler %q", value)
}
