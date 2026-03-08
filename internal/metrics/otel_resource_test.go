package metrics

import (
	"errors"
	"testing"

	"github.com/pdrhp/gitlab-elt/internal/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func TestBuildResource_Defaults(t *testing.T) {
	res, err := BuildResource(config.OTelResourceConfig{ServiceName: "gitlab-elt-worker", Environment: "production"})
	if err != nil {
		t.Fatalf("BuildResource error: %v", err)
	}
	assertResourceHas(t, res, map[attribute.Key]string{
		semconv.ServiceNameKey:                  "gitlab-elt-worker",
		attribute.Key("deployment.environment"): "production",
	})
}

func TestBuildResource_Overrides(t *testing.T) {
	cfg := config.OTelResourceConfig{
		ServiceName:    "custom",
		ServiceVersion: "1.2.3",
		Environment:    "staging",
		Namespace:      "etl",
		InstanceID:     "worker-1",
		Attributes:     map[string]string{"region": "us-west", "team": "data"},
	}
	res, err := BuildResource(cfg)
	if err != nil {
		t.Fatalf("BuildResource error: %v", err)
	}
	assertResourceHas(t, res, map[attribute.Key]string{
		semconv.ServiceNameKey:                  "custom",
		semconv.ServiceVersionKey:               "1.2.3",
		attribute.Key("deployment.environment"): "staging",
		semconv.ServiceNamespaceKey:             "etl",
		semconv.ServiceInstanceIDKey:            "worker-1",
		attribute.Key("region"):                 "us-west",
		attribute.Key("team"):                   "data",
	})
}

func TestBuildResource_InstanceFallback(t *testing.T) {
	oldHostname := hostnameFunc
	defer func() { hostnameFunc = oldHostname }()
	hostnameFunc = func() (string, error) {
		return "host-a", nil
	}
	res, err := BuildResource(config.OTelResourceConfig{})
	if err != nil {
		t.Fatalf("BuildResource error: %v", err)
	}
	assertResourceHas(t, res, map[attribute.Key]string{
		semconv.ServiceInstanceIDKey: "host-a",
	})

	// Simulate hostname error -> no instance attr
	hostnameFunc = func() (string, error) {
		return "", errors.New("fail")
	}
	res, err = BuildResource(config.OTelResourceConfig{})
	if err != nil {
		t.Fatalf("BuildResource error: %v", err)
	}
	assertResourceMissing(t, res, semconv.ServiceInstanceIDKey)
}

func assertResourceHas(t *testing.T, res *resource.Resource, expected map[attribute.Key]string) {
	t.Helper()
	attrs := res.Attributes()
	for key, value := range expected {
		found := false
		for _, attr := range attrs {
			if attr.Key == key && attr.Value.AsString() == value {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("attribute %s=%s not found", key, value)
		}
	}
}

func assertResourceMissing(t *testing.T, res *resource.Resource, key attribute.Key) {
	t.Helper()
	for _, attr := range res.Attributes() {
		if attr.Key == key {
			t.Fatalf("attribute %s unexpectedly present", key)
		}
	}
}
