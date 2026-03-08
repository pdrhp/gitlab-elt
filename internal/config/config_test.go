package config_test

import (
	"os"
	"testing"

	"github.com/pdrhp/gitlab-elt/internal/config"
)

func setRequiredEnvVars(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_USER", "test")
	t.Setenv("POSTGRES_PASSWORD", "test")
	t.Setenv("POSTGRES_DB", "test")
	t.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com")
	t.Setenv("GITLAB_TOKEN", "test-token")
	t.Setenv("GITLAB_PROJECT_IDS", "1,2,3")
}

func TestLoad_FromEnvVars(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Postgres.Host != "localhost" {
		t.Errorf("expected host localhost, got %s", cfg.Postgres.Host)
	}
	if cfg.Gitlab.BaseURL != "https://gitlab.example.com" {
		t.Errorf("expected base url https://gitlab.example.com, got %s", cfg.Gitlab.BaseURL)
	}
	if len(cfg.Gitlab.ProjectIDs) != 3 {
		t.Fatalf("expected 3 project IDs, got %d", len(cfg.Gitlab.ProjectIDs))
	}
	if cfg.Gitlab.ProjectIDs[0] != 1 {
		t.Errorf("expected first project ID 1, got %d", cfg.Gitlab.ProjectIDs[0])
	}
	if cfg.Worker.HealthPort != 8080 {
		t.Errorf("expected default health port 8080, got %d", cfg.Worker.HealthPort)
	}
}

func TestLoad_OTelMetricsDefaults(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	metricsCfg := cfg.Worker.OTel.Metrics
	if metricsCfg.Endpoint != "" {
		t.Fatalf("expected empty OTEL endpoint, got %q", metricsCfg.Endpoint)
	}
	if len(metricsCfg.Headers) != 0 {
		t.Fatalf("expected zero OTEL headers, got %v", metricsCfg.Headers)
	}
	if metricsCfg.Insecure {
		t.Fatalf("expected OTEL insecure=false by default")
	}
}

func TestLoad_OTelMetricsOverrides(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("OTEL_METRICS_ENDPOINT", "https://alloy:4318")
	t.Setenv("OTEL_METRICS_HEADERS", "Authorization=Bearer abc, X-Scope-OrgID=dev")
	t.Setenv("OTEL_METRICS_INSECURE", "true")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	metricsCfg := cfg.Worker.OTel.Metrics
	if metricsCfg.Endpoint != "https://alloy:4318" {
		t.Fatalf("unexpected endpoint: %q", metricsCfg.Endpoint)
	}
	if metricsCfg.Headers["Authorization"] != "Bearer abc" {
		t.Fatalf("missing Authorization header: %+v", metricsCfg.Headers)
	}
	if metricsCfg.Headers["X-Scope-OrgID"] != "dev" {
		t.Fatalf("missing org header: %+v", metricsCfg.Headers)
	}
	if !metricsCfg.Insecure {
		t.Fatalf("expected insecure flag true")
	}
}

func TestLoad_OTelMetricsHeadersInvalidEntry(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("OTEL_METRICS_ENDPOINT", "https://alloy:4318")
	t.Setenv("OTEL_METRICS_HEADERS", "invalid-entry")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid OTEL_METRICS_HEADERS entry")
	}
}

func TestLoad_OTelTracingDefaults(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	tracingCfg := cfg.Worker.OTel.Tracing
	if tracingCfg.Endpoint != "" {
		t.Fatalf("expected empty tracing endpoint, got %q", tracingCfg.Endpoint)
	}
	if len(tracingCfg.Headers) != 0 {
		t.Fatalf("expected zero tracing headers, got %v", tracingCfg.Headers)
	}
	if tracingCfg.Insecure {
		t.Fatalf("expected tracing insecure=false by default")
	}
	if tracingCfg.Sampler != "parentbased_always_on" {
		t.Fatalf("unexpected sampler default %q", tracingCfg.Sampler)
	}
}

func TestLoad_OTelTracingOverrides(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("OTEL_TRACES_ENDPOINT", "https://tempo:4318")
	t.Setenv("OTEL_TRACES_HEADERS", "Authorization=Bearer abc, X-Tenant=data")
	t.Setenv("OTEL_TRACES_INSECURE", "true")
	t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio:0.5")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	tracingCfg := cfg.Worker.OTel.Tracing
	if tracingCfg.Endpoint != "https://tempo:4318" {
		t.Fatalf("unexpected tracing endpoint: %q", tracingCfg.Endpoint)
	}
	if tracingCfg.Headers["Authorization"] != "Bearer abc" {
		t.Fatalf("missing Authorization header: %+v", tracingCfg.Headers)
	}
	if tracingCfg.Headers["X-Tenant"] != "data" {
		t.Fatalf("missing X-Tenant header: %+v", tracingCfg.Headers)
	}
	if !tracingCfg.Insecure {
		t.Fatalf("expected tracing insecure flag true")
	}
	if tracingCfg.Sampler != "traceidratio:0.5" {
		t.Fatalf("unexpected sampler value: %q", tracingCfg.Sampler)
	}
}

func TestLoad_OTelTracingInvalidHeaders(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("OTEL_TRACES_ENDPOINT", "https://tempo:4318")
	t.Setenv("OTEL_TRACES_HEADERS", "broken-entry")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for invalid OTEL_TRACES_HEADERS entry")
	}
}

func TestLoad_OTelTracingInvalidSampler(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("OTEL_TRACES_SAMPLER", "sometimes?")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected error for invalid OTEL_TRACES_SAMPLER")
	}
}

func TestLoad_OTelResourceDefaults(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	res := cfg.Worker.OTel.Resource
	if res.ServiceName != "gitlab-elt-worker" {
		t.Fatalf("unexpected service name: %q", res.ServiceName)
	}
	if res.ServiceVersion != "" {
		t.Fatalf("expected empty version, got %q", res.ServiceVersion)
	}
	if res.Environment != "production" || res.Namespace != "" || res.InstanceID != "" {
		t.Fatalf("expected empty optional fields, got %+v", res)
	}
	if len(res.Attributes) != 0 {
		t.Fatalf("expected empty attributes, got %v", res.Attributes)
	}
}

func TestLoad_OTelResourceOverrides(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("OTEL_SERVICE_NAME", "custom-worker")
	t.Setenv("OTEL_SERVICE_VERSION", "1.2.3")
	t.Setenv("OTEL_ENVIRONMENT", "staging")
	t.Setenv("OTEL_SERVICE_NAMESPACE", "gitlab")
	t.Setenv("OTEL_SERVICE_INSTANCE_ID", "worker-42")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "region=us-west-2, team=data")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	res := cfg.Worker.OTel.Resource
	if res.ServiceName != "custom-worker" {
		t.Fatalf("unexpected service name: %q", res.ServiceName)
	}
	if res.ServiceVersion != "1.2.3" {
		t.Fatalf("unexpected service version: %q", res.ServiceVersion)
	}
	if res.Environment != "staging" {
		t.Fatalf("unexpected environment: %q", res.Environment)
	}
	if res.Namespace != "gitlab" {
		t.Fatalf("unexpected namespace: %q", res.Namespace)
	}
	if res.InstanceID != "worker-42" {
		t.Fatalf("unexpected instance id: %q", res.InstanceID)
	}
	if len(res.Attributes) != 2 || res.Attributes["region"] != "us-west-2" || res.Attributes["team"] != "data" {
		t.Fatalf("unexpected attributes: %+v", res.Attributes)
	}
}

func TestLoad_OTelResourceInvalidAttributes(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "broken")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid OTEL_RESOURCE_ATTRIBUTES entry")
	}
}

func TestLoad_MissingRequiredVar(t *testing.T) {
	os.Clearenv()

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for missing required env vars, got nil")
	}
}

func TestLoad_InvalidHealthPort(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("HEALTH_PORT", "not-int")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid HEALTH_PORT, got nil")
	}
}

func TestLoad_MissingProjectSource(t *testing.T) {
	// Set all required vars EXCEPT project sources
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_USER", "test")
	t.Setenv("POSTGRES_PASSWORD", "test")
	t.Setenv("POSTGRES_DB", "test")
	t.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com")
	t.Setenv("GITLAB_TOKEN", "test-token")
	// Don't set GITLAB_PROJECT_IDS or GITLAB_GROUP_IDS

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when neither GITLAB_PROJECT_IDS nor GITLAB_GROUP_IDS is configured")
	}
}

func TestLoad_OnlyGroupsNoProjects(t *testing.T) {
	// Set all required vars with only groups (no projects)
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_USER", "test")
	t.Setenv("POSTGRES_PASSWORD", "test")
	t.Setenv("POSTGRES_DB", "test")
	t.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com")
	t.Setenv("GITLAB_TOKEN", "test-token")
	// Don't set GITLAB_PROJECT_IDS
	t.Setenv("GITLAB_GROUP_IDS", "10,20,30")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error when GITLAB_GROUP_IDS is configured, got %v", err)
	}
	if len(cfg.Gitlab.GroupIDs) != 3 {
		t.Errorf("expected 3 group IDs, got %d", len(cfg.Gitlab.GroupIDs))
	}
	if len(cfg.Gitlab.ProjectIDs) != 0 {
		t.Errorf("expected 0 project IDs, got %d", len(cfg.Gitlab.ProjectIDs))
	}
}

func TestLoad_GitlabGroupIDs(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("GITLAB_GROUP_IDS", "10,20,30")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(cfg.Gitlab.GroupIDs) != 3 {
		t.Fatalf("got GroupIDs=%v, want %v", cfg.Gitlab.GroupIDs, []int{10, 20, 30})
	}
	if cfg.Gitlab.GroupIDs[0] != 10 {
		t.Errorf("expected first group ID 10, got %d", cfg.Gitlab.GroupIDs[0])
	}
}

func TestLoad_GitlabGroupIDs_Empty(t *testing.T) {
	setRequiredEnvVars(t)
	// Don't set GITLAB_GROUP_IDS

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(cfg.Gitlab.GroupIDs) != 0 {
		t.Errorf("expected 0 group IDs when not set, got %d", len(cfg.Gitlab.GroupIDs))
	}
}

func TestLoad_RateLimitDefaults(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Gitlab.RateLimit != 20 {
		t.Errorf("expected default RateLimit 20, got %d", cfg.Gitlab.RateLimit)
	}
	if cfg.Gitlab.RetryMax != 3 {
		t.Errorf("expected default RetryMax 3, got %d", cfg.Gitlab.RetryMax)
	}
}

func TestLoad_RateLimitInvalid(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("GITLAB_RATE_LIMIT", "not-int")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when GITLAB_RATE_LIMIT is non-integer")
	}
}

func TestLoad_RateLimitCustom(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("GITLAB_RATE_LIMIT", "50")
	t.Setenv("GITLAB_RETRY_MAX", "5")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Gitlab.RateLimit != 50 {
		t.Errorf("expected RateLimit 50, got %d", cfg.Gitlab.RateLimit)
	}
	if cfg.Gitlab.RetryMax != 5 {
		t.Errorf("expected RetryMax 5, got %d", cfg.Gitlab.RetryMax)
	}
}

func TestLoad_SchedulerDefaults(t *testing.T) {
	setRequiredEnvVars(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Scheduler.SyncPeak != "*/15 * * * *" {
		t.Errorf("got SyncPeak=%q, want '*/15 * * * *'", cfg.Scheduler.SyncPeak)
	}
	if cfg.Scheduler.SyncOffPeak != "0 */4 * * *" {
		t.Errorf("got SyncOffPeak=%q, want '0 */4 * * *'", cfg.Scheduler.SyncOffPeak)
	}
	if cfg.Scheduler.Discovery != "0 */6 * * *" {
		t.Errorf("got Discovery=%q, want '0 */6 * * *'", cfg.Scheduler.Discovery)
	}
}

func TestLoad_SchedulerCustom(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("SCHEDULER_SYNC_PEAK", "*/5 * * * *")
	t.Setenv("SCHEDULER_SYNC_OFFPEAK", "0 */2 * * *")
	t.Setenv("SCHEDULER_DISCOVERY", "0 */12 * * *")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Scheduler.SyncPeak != "*/5 * * * *" {
		t.Errorf("expected SyncPeak */5 * * * *, got %s", cfg.Scheduler.SyncPeak)
	}
	if cfg.Scheduler.SyncOffPeak != "0 */2 * * *" {
		t.Errorf("expected SyncOffPeak 0 */2 * * *, got %s", cfg.Scheduler.SyncOffPeak)
	}
	if cfg.Scheduler.Discovery != "0 */12 * * *" {
		t.Errorf("expected Discovery 0 */12 * * *, got %s", cfg.Scheduler.Discovery)
	}
}

func TestPostgresConfig_DSN(t *testing.T) {
	pg := config.PostgresConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "user",
		Password: "pass",
		DB:       "mydb",
	}

	expected := "postgres://user:pass@localhost:5432/mydb?sslmode=disable"
	if pg.DSN() != expected {
		t.Errorf("expected DSN %s, got %s", expected, pg.DSN())
	}
}
