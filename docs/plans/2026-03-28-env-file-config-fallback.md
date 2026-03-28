# Environment Variable `_FILE` Fallback Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add config loading support that reads `*_FILE` values when direct env vars are empty, while keeping direct env values as higher priority.

**Architecture:** Centralize env resolution in small helper functions inside `internal/config/config.go` so all config parsing paths can share a single precedence rule: direct env first, then file fallback. Keep current validation behavior intact (required values still required, type parsing still strict), but feed those validations with resolved values from the new helper. Cover behavior with table-driven tests for precedence, file-read success, and file-read failures.

**Tech Stack:** Go, standard library (`os`, `strings`, `strconv`, `fmt`), existing `testing` package, existing config module.

---

### Task 1: Define behavior with failing tests first

**Files:**
- Modify: `internal/config/config_test.go`
- Test: `internal/config/config_test.go`

**Step 1: Write failing test for `_FILE` fallback used when direct env is empty**

```go
func TestLoad_UsesFileFallbackForRequiredSecrets(t *testing.T) {
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_DB", "test")
	t.Setenv("GITLAB_BASE_URL", "https://gitlab.example.com")
	t.Setenv("GITLAB_PROJECT_IDS", "1")

	userFile := writeTempFile(t, "db-user-from-file\n")
	passFile := writeTempFile(t, "db-pass-from-file\n")
	tokenFile := writeTempFile(t, "gitlab-token-from-file\n")
	t.Setenv("POSTGRES_USER_FILE", userFile)
	t.Setenv("POSTGRES_PASSWORD_FILE", passFile)
	t.Setenv("GITLAB_TOKEN_FILE", tokenFile)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Postgres.User != "db-user-from-file" || cfg.Postgres.Password != "db-pass-from-file" || cfg.Gitlab.Token != "gitlab-token-from-file" {
		t.Fatalf("expected values loaded from *_FILE, got %+v", cfg)
	}
}
```

**Step 2: Write failing test for precedence (direct env must win over `_FILE`)**

```go
func TestLoad_DirectEnvHasPriorityOverFileFallback(t *testing.T) {
	setRequiredEnvVars(t)
	tokenFile := writeTempFile(t, "token-from-file")
	t.Setenv("GITLAB_TOKEN", "token-from-env")
	t.Setenv("GITLAB_TOKEN_FILE", tokenFile)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Gitlab.Token != "token-from-env" {
		t.Fatalf("expected env priority, got %q", cfg.Gitlab.Token)
	}
}
```

**Step 3: Write failing test for invalid `_FILE` path error**

```go
func TestLoad_FileFallbackReadError(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN_FILE", "/path/that/does/not/exist")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when *_FILE cannot be read")
	}
}
```

**Step 4: Add small test helper for temp file content**

```go
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "cfg-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}
	return f.Name()
}
```

**Step 5: Run targeted tests to confirm they fail before implementation**

Run: `go test ./internal/config -run 'FileFallback|Priority|UsesFileFallback' -v`
Expected: FAIL with current implementation not loading `*_FILE`.

**Step 6: Commit test scaffolding**

```bash
git add internal/config/config_test.go
git commit -m "test: define env file fallback behavior for config loading"
```

### Task 2: Implement env resolution helper with precedence and file read

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Step 1: Add helper that resolves direct env first, then `<KEY>_FILE`**

```go
func resolveEnvValue(key string) (string, error) {
	if val := os.Getenv(key); val != "" {
		return val, nil
	}

	fileKey := key + "_FILE"
	filePath := strings.TrimSpace(os.Getenv(fileKey))
	if filePath == "" {
		return "", nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed reading %s (%s): %w", fileKey, filePath, err)
	}

	return strings.TrimSpace(string(content)), nil
}
```

**Step 2: Update required string loader to use resolver and propagate read errors**

```go
func requireEnv(key string) string {
	val, _ := resolveEnvValue(key)
	return val
}

func requireEnvStrict(key string) (string, error) {
	val, err := resolveEnvValue(key)
	if err != nil {
		return "", err
	}
	if val == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return val, nil
}
```

**Step 3: Update integer/default helpers to parse resolved values**

```go
func requireEnvInt(key string) (int, error) {
	val, err := resolveEnvValue(key)
	if err != nil {
		return 0, err
	}
	if val == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	return strconv.Atoi(val)
}

func envOrDefault(key, defaultVal string) string {
	val, err := resolveEnvValue(key)
	if err != nil || val == "" {
		return defaultVal
	}
	return val
}
```

**Step 4: Wire `Load()` to strict helpers for required fields**

```go
pgHost, err := requireEnvStrict("POSTGRES_HOST")
if err != nil {
	return nil, err
}
// repeat for POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB,
// GITLAB_BASE_URL, GITLAB_TOKEN
```

**Step 5: Run targeted test set**

Run: `go test ./internal/config -run 'Load_(UsesFileFallback|DirectEnvHasPriority|FileFallbackReadError|FromEnvVars)' -v`
Expected: PASS.

**Step 6: Commit implementation**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: support *_FILE fallback for config env values"
```

### Task 3: Expand coverage to optional/list/integer settings that may use `_FILE`

**Files:**
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go` (if needed after tests)
- Test: `internal/config/config_test.go`

**Step 1: Add failing test for list fallback (`GITLAB_PROJECT_IDS_FILE`)**

```go
func TestLoad_ProjectIDsFromFileFallback(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("GITLAB_PROJECT_IDS", "")
	file := writeTempFile(t, "101,202,303")
	t.Setenv("GITLAB_PROJECT_IDS_FILE", file)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cfg.Gitlab.ProjectIDs) != 3 || cfg.Gitlab.ProjectIDs[0] != 101 {
		t.Fatalf("unexpected ids: %v", cfg.Gitlab.ProjectIDs)
	}
}
```

**Step 2: Add failing test for optional int fallback (`HEALTH_PORT_FILE`)**

```go
func TestLoad_HealthPortFromFileFallback(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("HEALTH_PORT", "")
	t.Setenv("HEALTH_PORT_FILE", writeTempFile(t, "9090"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Worker.HealthPort != 9090 {
		t.Fatalf("expected 9090, got %d", cfg.Worker.HealthPort)
	}
}
```

**Step 3: Implement minimal fixes only if tests still fail**

```go
// Replace remaining os.Getenv(...) reads in Load() and helper loaders
// with resolveEnvValue(...) where appropriate.
```

**Step 4: Run full config package tests**

Run: `go test ./internal/config -v`
Expected: PASS.

**Step 5: Commit expanded behavior**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "test: cover *_FILE fallback for optional and list config"
```

### Task 4: Document `_FILE` usage for container secret injection

**Files:**
- Modify: `.env.example`

**Step 1: Add commented examples for direct and `_FILE` options**

```dotenv
# Direct env has priority over *_FILE when both are set.
# Use *_FILE for Docker/Kubernetes secret mounts.
POSTGRES_USER=
POSTGRES_USER_FILE=
POSTGRES_PASSWORD=
POSTGRES_PASSWORD_FILE=
GITLAB_TOKEN=
GITLAB_TOKEN_FILE=
```

**Step 2: Keep examples consistent with existing variable names (`POSTGRES_*`, not `DB_*`)**

```dotenv
# Keep this project naming convention:
# POSTGRES_USER / POSTGRES_USER_FILE
# POSTGRES_PASSWORD / POSTGRES_PASSWORD_FILE
```

**Step 3: Run formatting/lint sanity (if configured)**

Run: `go test ./internal/config -run TestLoad_FromEnvVars -v`
Expected: PASS.

**Step 4: Commit docs update**

```bash
git add .env.example
git commit -m "docs: document *_FILE config fallback for secrets"
```

### Task 5: Final verification and integration checks

**Files:**
- Verify only: `internal/config/config.go`
- Verify only: `internal/config/config_test.go`
- Verify only: `.env.example`

**Step 1: Run complete test suite or relevant module suites**

Run: `go test ./...`
Expected: PASS (or known unrelated failures documented).

**Step 2: Verify precedence behavior manually in one focused run**

Run: `go test ./internal/config -run TestLoad_DirectEnvHasPriorityOverFileFallback -v`
Expected: PASS and confirms direct env beats file fallback.

**Step 3: Check git status for clean staged changes per commit intent**

Run: `git status`
Expected: clean working tree, or only intentional unstaged changes.

**Step 4: Prepare MR/PR description points**

```text
- Added generic env resolver supporting <KEY> and <KEY>_FILE precedence.
- Added tests for file fallback, precedence, and error paths.
- Documented container secret injection pattern with *_FILE variables.
```
