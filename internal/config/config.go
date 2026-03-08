package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Postgres  PostgresConfig
	Gitlab    GitlabConfig
	Worker    WorkerConfig
	Scheduler SchedulerConfig
}

type PostgresConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DB       string
}

func (p PostgresConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		p.User, p.Password, p.Host, p.Port, p.DB)
}

type GitlabConfig struct {
	BaseURL    string
	Token      string
	ProjectIDs []int
	GroupIDs   []int
	RateLimit  int
	RetryMax   int
}

type WorkerConfig struct {
	HealthPort int
	OTel       WorkerOTelConfig
}

type WorkerOTelConfig struct {
	Metrics  OTelMetricsConfig
	Resource OTelResourceConfig
	Tracing  OTelTracingConfig
}

type OTelMetricsConfig struct {
	Endpoint string
	Headers  map[string]string
	Insecure bool
}

type OTelTracingConfig struct {
	Endpoint string
	Headers  map[string]string
	Insecure bool
	Sampler  string
}

type OTelResourceConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Namespace      string
	InstanceID     string
	Attributes     map[string]string
}

const defaultOTelTracingSampler = "parentbased_always_on"

// DefaultOTelTracingSampler exposes the default sampler used when
// OTEL_TRACES_SAMPLER is not set.
const DefaultOTelTracingSampler = defaultOTelTracingSampler

type SchedulerConfig struct {
	SyncPeak    string
	SyncOffPeak string
	Discovery   string
}

func Load() (*Config, error) {
	// Attempt to load .env file; ignore error if file doesn't exist
	_ = godotenv.Load()

	pgPort, err := requireEnvInt("POSTGRES_PORT")
	if err != nil {
		return nil, err
	}

	// Parse optional GITLAB_PROJECT_IDS
	var projectIDs []int
	if projectIDsStr := os.Getenv("GITLAB_PROJECT_IDS"); projectIDsStr != "" {
		projectIDs, err = parseIntList(projectIDsStr)
		if err != nil {
			return nil, fmt.Errorf("invalid GITLAB_PROJECT_IDS: %w", err)
		}
	}

	pgHost := requireEnv("POSTGRES_HOST")
	if pgHost == "" {
		return nil, fmt.Errorf("POSTGRES_HOST is required")
	}

	pgUser := requireEnv("POSTGRES_USER")
	if pgUser == "" {
		return nil, fmt.Errorf("POSTGRES_USER is required")
	}

	pgPassword := requireEnv("POSTGRES_PASSWORD")
	if pgPassword == "" {
		return nil, fmt.Errorf("POSTGRES_PASSWORD is required")
	}

	pgDB := requireEnv("POSTGRES_DB")
	if pgDB == "" {
		return nil, fmt.Errorf("POSTGRES_DB is required")
	}

	gitlabBaseURL := requireEnv("GITLAB_BASE_URL")
	if gitlabBaseURL == "" {
		return nil, fmt.Errorf("GITLAB_BASE_URL is required")
	}

	gitlabToken := requireEnv("GITLAB_TOKEN")
	if gitlabToken == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN is required")
	}

	// Parse optional GITLAB_GROUP_IDS
	var groupIDs []int
	if groupIDsStr := os.Getenv("GITLAB_GROUP_IDS"); groupIDsStr != "" {
		groupIDs, err = parseIntList(groupIDsStr)
		if err != nil {
			return nil, fmt.Errorf("invalid GITLAB_GROUP_IDS: %w", err)
		}
	}

	// Validate that at least one source is configured
	if len(projectIDs) == 0 && len(groupIDs) == 0 {
		return nil, fmt.Errorf("either GITLAB_PROJECT_IDS or GITLAB_GROUP_IDS must be configured")
	}

	// Parse rate limit and retry settings with defaults
	rateLimit, err := envOrDefaultInt("GITLAB_RATE_LIMIT", 20)
	if err != nil {
		return nil, fmt.Errorf("GITLAB_RATE_LIMIT must be an integer: %w", err)
	}

	retryMax, err := envOrDefaultInt("GITLAB_RETRY_MAX", 3)
	if err != nil {
		return nil, fmt.Errorf("GITLAB_RETRY_MAX must be an integer: %w", err)
	}

	healthPort := 8080
	if v := os.Getenv("HEALTH_PORT"); v != "" {
		healthPort, err = strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("HEALTH_PORT must be an integer: %w", err)
		}
	}

	// Parse scheduler configuration with defaults
	syncPeak := envOrDefault("SCHEDULER_SYNC_PEAK", "*/15 * * * *")
	syncOffPeak := envOrDefault("SCHEDULER_SYNC_OFFPEAK", "0 */4 * * *")
	discovery := envOrDefault("SCHEDULER_DISCOVERY", "0 */6 * * *")

	otelCfg, err := loadOTelMetricsConfig()
	if err != nil {
		return nil, err
	}

	otelResourceCfg, err := loadOTelResourceConfig()
	if err != nil {
		return nil, err
	}

	otelTracingCfg, err := loadOTelTracingConfig()
	if err != nil {
		return nil, err
	}

	return &Config{
		Postgres: PostgresConfig{
			Host:     pgHost,
			Port:     pgPort,
			User:     pgUser,
			Password: pgPassword,
			DB:       pgDB,
		},
		Gitlab: GitlabConfig{
			BaseURL:    gitlabBaseURL,
			Token:      gitlabToken,
			ProjectIDs: projectIDs,
			GroupIDs:   groupIDs,
			RateLimit:  rateLimit,
			RetryMax:   retryMax,
		},
		Worker: WorkerConfig{
			HealthPort: healthPort,
			OTel: WorkerOTelConfig{
				Metrics:  otelCfg,
				Resource: otelResourceCfg,
				Tracing:  otelTracingCfg,
			},
		},
		Scheduler: SchedulerConfig{
			SyncPeak:    syncPeak,
			SyncOffPeak: syncOffPeak,
			Discovery:   discovery,
		},
	}, nil
}

func requireEnv(key string) string {
	return os.Getenv(key)
}

func requireEnvInt(key string) (int, error) {
	val := os.Getenv(key)
	if val == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return n, nil
}

func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func envOrDefaultInt(key string, defaultVal int) (int, error) {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal, nil
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func parseIntList(s string) ([]int, error) {
	if s == "" {
		return nil, fmt.Errorf("empty list")
	}
	parts := strings.Split(s, ",")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q: %w", p, err)
		}
		result = append(result, n)
	}
	return result, nil
}

func loadOTelMetricsConfig() (OTelMetricsConfig, error) {
	cfg := OTelMetricsConfig{}
	endpoint := strings.TrimSpace(os.Getenv("OTEL_METRICS_ENDPOINT"))
	if endpoint == "" {
		return cfg, nil
	}
	cfg.Endpoint = endpoint

	if headersEnv := strings.TrimSpace(os.Getenv("OTEL_METRICS_HEADERS")); headersEnv != "" {
		headers, err := parseOTelHeaders(headersEnv, "OTEL_METRICS_HEADERS")
		if err != nil {
			return OTelMetricsConfig{}, err
		}
		cfg.Headers = headers
	}

	if v := strings.TrimSpace(os.Getenv("OTEL_METRICS_INSECURE")); v != "" {
		insecure, err := strconv.ParseBool(v)
		if err != nil {
			return OTelMetricsConfig{}, fmt.Errorf("OTEL_METRICS_INSECURE must be a boolean: %w", err)
		}
		cfg.Insecure = insecure
	}

	return cfg, nil
}

func loadOTelTracingConfig() (OTelTracingConfig, error) {
	cfg := OTelTracingConfig{}
	cfg.Endpoint = strings.TrimSpace(os.Getenv("OTEL_TRACES_ENDPOINT"))

	if headersEnv := strings.TrimSpace(os.Getenv("OTEL_TRACES_HEADERS")); headersEnv != "" {
		headers, err := parseOTelHeaders(headersEnv, "OTEL_TRACES_HEADERS")
		if err != nil {
			return OTelTracingConfig{}, err
		}
		cfg.Headers = headers
	}

	if v := strings.TrimSpace(os.Getenv("OTEL_TRACES_INSECURE")); v != "" {
		insecure, err := strconv.ParseBool(v)
		if err != nil {
			return OTelTracingConfig{}, fmt.Errorf("OTEL_TRACES_INSECURE must be a boolean: %w", err)
		}
		cfg.Insecure = insecure
	}

	sampler := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER"))
	if sampler == "" {
		sampler = defaultOTelTracingSampler
	}
	if err := validateOTelSampler(sampler); err != nil {
		return OTelTracingConfig{}, err
	}
	cfg.Sampler = sampler

	return cfg, nil
}

func loadOTelResourceConfig() (OTelResourceConfig, error) {
	cfg := OTelResourceConfig{
		ServiceName: "gitlab-elt-worker",
		Environment: "production",
		Attributes:  map[string]string{},
	}

	if v := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); v != "" {
		cfg.ServiceName = v
	}
	cfg.ServiceVersion = strings.TrimSpace(os.Getenv("OTEL_SERVICE_VERSION"))
	if v := strings.TrimSpace(os.Getenv("OTEL_ENVIRONMENT")); v != "" {
		cfg.Environment = v
	}
	cfg.Namespace = strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAMESPACE"))
	cfg.InstanceID = strings.TrimSpace(os.Getenv("OTEL_SERVICE_INSTANCE_ID"))

	if attrs := strings.TrimSpace(os.Getenv("OTEL_RESOURCE_ATTRIBUTES")); attrs != "" {
		pairs := strings.Split(attrs, ",")
		for _, pair := range pairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) != 2 {
				return OTelResourceConfig{}, fmt.Errorf("invalid OTEL_RESOURCE_ATTRIBUTES entry %q", pair)
			}
			key := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			if key == "" || value == "" {
				return OTelResourceConfig{}, fmt.Errorf("invalid OTEL_RESOURCE_ATTRIBUTES entry %q", pair)
			}
			cfg.Attributes[key] = value
		}
	}

	return cfg, nil
}

func parseOTelHeaders(value, envKey string) (map[string]string, error) {
	result := make(map[string]string)
	pairs := strings.Split(value, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" {
			return nil, fmt.Errorf("invalid %s entry %q", envKey, pair)
		}
		result[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return result, nil
}

func validateOTelSampler(value string) error {
	switch value {
	case "always_on", "always_off", defaultOTelTracingSampler:
		return nil
	}
	const ratioPrefix = "traceidratio:"
	if strings.HasPrefix(value, ratioPrefix) {
		ratioStr := strings.TrimSpace(strings.TrimPrefix(value, ratioPrefix))
		if ratioStr == "" {
			return fmt.Errorf("invalid OTEL_TRACES_SAMPLER %q: missing ratio", value)
		}
		ratio, err := strconv.ParseFloat(ratioStr, 64)
		if err != nil {
			return fmt.Errorf("invalid OTEL_TRACES_SAMPLER %q: %w", value, err)
		}
		if ratio < 0 || ratio > 1 {
			return fmt.Errorf("invalid OTEL_TRACES_SAMPLER %q: ratio must be between 0 and 1", value)
		}
		return nil
	}
	return fmt.Errorf("invalid OTEL_TRACES_SAMPLER %q", value)
}
