package metrics

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// BuildOTLPMeter configures an OTLP/HTTP metrics exporter and corresponding meter
// using the provided configuration. When cfg.Endpoint is empty, OTLP is disabled
// and the return values are (nil, nil, nil).
func BuildOTLPMeter(ctx context.Context, cfg config.OTelMetricsConfig) (OTLPMeter, func(context.Context) error, error) {
	if cfg.Endpoint == "" {
		return nil, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	useEndpointURL := strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://")
	opts := []otlpmetrichttp.Option{}
	if useEndpointURL {
		opts = append(opts, otlpmetrichttp.WithEndpointURL(endpoint))
	} else {
		opts = append(opts, otlpmetrichttp.WithEndpoint(endpoint))
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(cfg.Headers))
	}
	if cfg.Insecure {
		if strings.HasPrefix(endpoint, "https://") {
			opts = append(opts, otlpmetrichttp.WithTLSClientConfig(&tls.Config{InsecureSkipVerify: true}))
		} else {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
	}

	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("create OTLP metrics exporter: %w", err)
	}

	reader := sdkmetric.NewPeriodicReader(exporter)
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	shutdown := func(shutdownCtx context.Context) error {
		return provider.Shutdown(shutdownCtx)
	}

	meter := provider.Meter("gitlab-elt-worker")
	otlpMeter, err := NewOTLPMeter(meter, ctx)
	if err != nil {
		_ = shutdown(context.Background())
		return nil, nil, err
	}

	return otlpMeter, shutdown, nil
}
