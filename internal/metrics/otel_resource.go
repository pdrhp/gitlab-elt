package metrics

import (
	"os"
	"sort"
	"strings"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var hostnameFunc = os.Hostname

func BuildResource(cfg config.OTelResourceConfig) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
	}
	if v := strings.TrimSpace(cfg.ServiceVersion); v != "" {
		attrs = append(attrs, semconv.ServiceVersion(v))
	}
	if v := strings.TrimSpace(cfg.Environment); v != "" {
		attrs = append(attrs, attribute.String("deployment.environment", v))
	}
	if v := strings.TrimSpace(cfg.Namespace); v != "" {
		attrs = append(attrs, semconv.ServiceNamespace(v))
	}
	instanceID := strings.TrimSpace(cfg.InstanceID)
	if instanceID == "" {
		if host, err := hostnameFunc(); err == nil {
			instanceID = host
		}
	}
	if instanceID != "" {
		attrs = append(attrs, semconv.ServiceInstanceID(instanceID))
	}
	// deterministic ordering for extra attributes
	extraKeys := make([]string, 0, len(cfg.Attributes))
	for k := range cfg.Attributes {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		attrs = append(attrs, attribute.String(k, cfg.Attributes[k]))
	}

	return resource.NewWithAttributes(semconv.SchemaURL, attrs...), nil
}
