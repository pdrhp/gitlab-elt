# OTEL Resource Metadata Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Introduce configuration + helpers so OTEL resources expose worker metadata in emitted metrics.

**Architecture:** Expand the worker config so OTEL resource metadata is parsed from env vars and consumed by a dedicated builder that merges defaults with overrides before constructing the OTEL resource. The metrics package will rely on this helper to ensure consistent attributes wherever resources are required.

**Tech Stack:** Go 1.21+, `go.opentelemetry.io/otel/sdk/resource`, `go.opentelemetry.io/otel/semconv/v1.26.0`.

---

### Task 1: Extend worker config with OTEL resource metadata

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Define config types**

```go
type OTelResourceConfig struct {
    ServiceName    string
    ServiceVersion string
    Environment    string
    Namespace      string
    InstanceID     string
    Attributes     map[string]string
}

type WorkerConfig struct {
    HealthPort int
    OTel struct {
        Metrics  OTelMetricsConfig
        Resource OTelResourceConfig
    }
}
```

**Step 2: Load resource env vars**

```go
func loadOTelResourceConfig() (OTelResourceConfig, error) {
    cfg := OTelResourceConfig{
        ServiceName: "gitlab-elt-worker",
        Attributes:   map[string]string{},
    }
    // fallback for other strings is empty
    cfg.ServiceName = envOrDefault("OTEL_SERVICE_NAME", cfg.ServiceName)
    cfg.ServiceVersion = envOrDefault("OTEL_SERVICE_VERSION", "")
    cfg.Environment = envOrDefault("OTEL_ENVIRONMENT", "")
    cfg.Namespace = envOrDefault("OTEL_SERVICE_NAMESPACE", "")
    cfg.InstanceID = envOrDefault("OTEL_SERVICE_INSTANCE_ID", "")
    if attrs := strings.TrimSpace(os.Getenv("OTEL_RESOURCE_ATTRIBUTES")); attrs != "" {
        for _, pair := range strings.Split(attrs, ",") {
            kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
            if len(kv) != 2 || kv[0] == "" || kv[1] == "" {
                return OTelResourceConfig{}, fmt.Errorf("invalid OTEL_RESOURCE_ATTRIBUTES entry %q", pair)
            }
            cfg.Attributes[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
        }
    }
    return cfg, nil
}
```

**Step 3: Wire into Load()**

```go
resourceCfg, err := loadOTelResourceConfig()
if err != nil {
    return nil, err
}

return &Config{ Worker: WorkerConfig{ OTel: struct {
        Metrics  OTelMetricsConfig
        Resource OTelResourceConfig
    }{ Metrics: otelCfg, Resource: resourceCfg },
}}, nil
```

**Step 4: Tests for defaults + overrides + invalid attrs**

- `TestLoad_OTelResourceDefaults` ensures default service name + empty others + empty attr map.
- `TestLoad_OTelResourceOverrides` sets env vars for every field plus `OTEL_RESOURCE_ATTRIBUTES="region=us-east-1,team=data"` and asserts values.
- `TestLoad_OTelResourceInvalidAttributes` sets `OTEL_RESOURCE_ATTRIBUTES="broken"` and expects `config.Load` error.

**Step 5: Run targeted tests**

Run: `GOROOT=/home/pedrohenrique/.goenv/versions/1.25.7 go test ./internal/config -v`
Expected: All tests PASS.

### Task 2: Build OTEL resource helper

**Files:**
- Create: `internal/metrics/otel_resource.go`
- Create: `internal/metrics/otel_resource_test.go`

**Step 1: Implement builder**

```go
package metrics

import (
    "os"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func BuildResource(cfg config.OTelResourceConfig) (*resource.Resource, error) {
    attrs := []attribute.KeyValue{
        semconv.ServiceNameKey.String(defaultIfEmpty(cfg.ServiceName, "gitlab-elt-worker")),
    }
    if v := strings.TrimSpace(cfg.ServiceVersion); v != "" {
        attrs = append(attrs, semconv.ServiceVersionKey.String(v))
    }
    if v := strings.TrimSpace(cfg.Environment); v != "" {
        attrs = append(attrs, attribute.String("deployment.environment", v))
    }
    if v := strings.TrimSpace(cfg.Namespace); v != "" {
        attrs = append(attrs, semconv.ServiceNamespaceKey.String(v))
    }
    instanceID := strings.TrimSpace(cfg.InstanceID)
    if instanceID == "" {
        if host, err := os.Hostname(); err == nil {
            instanceID = host
        }
    }
    if instanceID != "" {
        attrs = append(attrs, semconv.ServiceInstanceIDKey.String(instanceID))
    }
    keys := make([]string, 0, len(cfg.Attributes))
    for k := range cfg.Attributes {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    for _, k := range keys {
        attrs = append(attrs, attribute.String(k, cfg.Attributes[k]))
    }
    return resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, attrs...))
}
```

**Step 2: Unit tests**

```go
func TestBuildResource_Defaults(t *testing.T) {
    res, err := BuildResource(config.OTelResourceConfig{})
    require.NoError(t, err)
    assertResourceHas(t, res, map[attribute.Key]string{ semconv.ServiceNameKey: "gitlab-elt-worker" })
}

func TestBuildResource_WithOverrides(t *testing.T) {
    cfg := config.OTelResourceConfig{ ServiceName: "custom", ServiceVersion: "1.2.3", Environment: "staging", Namespace: "etl", InstanceID: "worker-1", Attributes: map[string]string{"region":"us-west"}}
    res, err := BuildResource(cfg)
    require.NoError(t, err)
    assertResourceHas(t, res, map[attribute.Key]string{ semconv.ServiceNameKey: "custom", attribute.Key("deployment.environment"): "staging", attribute.Key("region"): "us-west" })
}

func TestBuildResource_InstanceFallback(t *testing.T) {
    t.Setenv("HOSTNAME", "test-host") // use stub via os.Hostname override or helper
}
```

- Build `assertResourceHas` helper to iterate `res.Attributes()` and compare needed keys.

**Step 3: Run metrics tests**

Run: `GOROOT=/home/pedrohenrique/.goenv/versions/1.25.7 go test ./internal/metrics -v`
Expected: PASS and new coverage around resource builder.

---

Plan complete and saved to `docs/plans/2026-03-07-otel-resource-config-impl.md`. Proceed with subagent-driven execution in this session per user request.
