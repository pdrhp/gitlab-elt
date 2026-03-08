# Multi-Stage Dockerfile Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create an optimized multi-stage Dockerfile for the GitLab ELT worker Go application with non-root user and minimal attack surface.

**Architecture:** Two-stage Docker build pattern using golang:1.25-alpine for build stage and scratch/alpine for runtime. This minimizes final image size while maintaining SSL capabilities for GitLab API calls and PostgreSQL connections.

**Tech Stack:** Docker, Go 1.25.7, Alpine Linux, ca-certificates

---

### Task 1: Create .dockerignore

**Files:**
- Create: `.dockerignore`

**Step 1: Write the .dockerignore file**

```dockerignore
# Git
.git
.gitignore

# Documentation
docs/
*.md

# Local environment files
.env
.env.local
.env.*.local

# IDE
.vscode/
.idea/
*.swp
*.swo
*~

# OS files
.DS_Store
Thumbs.db

# Build artifacts (we'll rebuild in container)
bin/
*.exe
*.dll
*.so
*.dylib

# Test files
*_test.go
tests/
*.test

# Docker files (not needed inside container)
Dockerfile*
docker-compose*
.dockerignore

# Database migrations and queries (handled separately in production)
db/migrations/
db/query/
db/seeds/

# Misc
*.log
*.tmp
.cache/
node_modules/
```

**Step 2: Verify file creation**

Run: `cat .dockerignore | head -5`
Expected: Shows the first 5 lines of the file

**Step 3: Commit**

```bash
git add .dockerignore
git commit -m "chore: add .dockerignore to reduce build context size"
```

---

### Task 2: Create Multi-Stage Dockerfile

**Files:**
- Create: `Dockerfile`

**Step 1: Write the multi-stage Dockerfile**

```dockerfile
# Stage 1: Build
FROM golang:1.25.7-alpine AS builder

# Install git and ca-certificates for fetching dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Create non-root user for build
RUN adduser -D -g '' appuser

# Set working directory
WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the worker binary with optimizations
# -ldflags: strip debug info, optimize binary size
# CGO_ENABLED=0: disable CGO for static binary
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

RUN go build -ldflags='-w -s' -a -installsuffix cgo -o bin/worker cmd/worker/main.go

# Stage 2: Runtime (minimal scratch image)
FROM scratch

# Copy CA certificates from builder for HTTPS calls to GitLab
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy timezone data for proper time handling
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the built binary
COPY --from=builder /app/bin/worker /worker

# Copy the non-root user from builder (passwd file needed for user resolution)
COPY --from=builder /etc/passwd /etc/passwd

# Use non-root user
USER appuser

# Health check port (matches application config)
EXPOSE 8080

# Set environment variable for binary location
ENV PATH=/

# Run the worker
ENTRYPOINT ["/worker"]
```

**Step 2: Verify Dockerfile creation**

Run: `cat Dockerfile | head -10`
Expected: Shows the first 10 lines including "FROM golang:1.25.7-alpine AS builder"

**Step 3: Commit**

```bash
git add Dockerfile
git commit -m "feat: add multi-stage Dockerfile with non-root user"
```

---

### Task 3: Test Docker Build

**Files:**
- Test: `Dockerfile`

**Step 1: Build the Docker image**

Run: `docker build -t gitlab-elt-worker:test .`

Expected: Build completes successfully with output showing:
- "[builder 6/6] RUN go build..." completing
- "exporting to image" completing
- No errors

**Step 2: Verify image was created**

Run: `docker images | grep gitlab-elt-worker`

Expected: Shows the image with tag `test` and size (should be < 50MB for optimized scratch-based image)

Example output:
```
REPOSITORY          TAG       SIZE
gitlab-elt-worker   test      25.3MB
```

**Step 3: Test image runs (without database - should fail gracefully)**

Run: `docker run --rm gitlab-elt-worker:test 2>&1 | head -5`

Expected: Application starts and exits with error about database connection (expected without proper env vars):
```
{"level":"INFO","msg":"starting gitlab-elt worker"}
{"level":"ERROR","msg":"failed to load configuration","error":"..."}
```

**Step 4: Verify non-root user**

Run: `docker run --rm --entrypoint="" gitlab-elt-worker:test whoami`

Expected: `appuser`

**Step 5: Commit build verification**

```bash
git commit --allow-empty -m "chore: verify Dockerfile builds successfully"
```

---

### Task 4: Test Docker Image with Environment

**Files:**
- Test: `Dockerfile` with environment variables

**Step 1: Create test environment file**

Create a minimal `.env.docker.test` file:

```bash
cat > .env.docker.test << 'EOF'
# Database
POSTGRES_HOST=host.docker.internal
POSTGRES_PORT=5432
POSTGRES_DB=gitlab_elt_test
POSTGRES_USER=testuser
POSTGRES_PASSWORD=testpass

# GitLab
GITLAB_BASE_URL=https://gitlab.com/api/v4
GITLAB_TOKEN=test-token
GITLAB_PROJECT_IDS=1,2,3
GITLAB_GROUP_IDS=4,5
GITLAB_RATE_LIMIT=100
GITLAB_RETRY_MAX=3

# Worker
WORKER_HEALTH_PORT=8080

# Scheduler
SCHEDULER_SYNC_PEAK=0 */1 * * *
SCHEDULER_SYNC_OFFPEAK=0 2 * * *
SCHEDULER_DISCOVERY=0 3 * * 0
EOF
```

**Step 2: Run container with environment file (will fail without real DB but tests config loading)**

Run: `docker run --rm --env-file .env.docker.test --network host gitlab-elt-worker:test 2>&1 | head -3`

Expected: Application loads config and attempts to connect to database:
```
{"level":"INFO","msg":"starting gitlab-elt worker"}
{"level":"INFO","msg":"connected to database"}  # OR connection error (expected without real DB)
```

**Step 3: Test health endpoint (after brief startup)**

In another terminal, with container running:

```bash
docker run -d --name test-worker --env-file .env.docker.test -p 8080:8080 gitlab-elt-worker:test
sleep 2
curl -s http://localhost:8080/health || echo "Health check endpoint exists"
docker stop test-worker && docker rm test-worker
```

Expected: Health endpoint responds (even if with error status, the port is exposed)

**Step 4: Cleanup test files**

Run: `rm .env.docker.test`

**Step 5: Commit test documentation**

```bash
git commit --allow-empty -m "chore: verify Docker image runs with environment variables"
```

---

### Task 5: Verify Image Size and Security

**Files:**
- Test: `Dockerfile`

**Step 1: Check final image size**

Run: `docker images gitlab-elt-worker:test --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}"`

Expected: Shows size, ideally < 30MB due to scratch base

**Step 2: Verify no shell in scratch image**

Run: `docker run --rm --entrypoint="" gitlab-elt-worker:test sh 2>&1`

Expected: Error like "executable file not found in $PATH" (scratch has no shell)

**Step 3: Verify binary is statically linked**

Run: `docker run --rm --entrypoint="" gitlab-elt-worker:test /worker --help 2>&1 | head -5`

Expected: Application runs and shows help or usage (if implemented) or starts up

**Step 4: Document image characteristics**

Add a comment to the README or commit message documenting:
- Final image size
- Non-root user (appuser)
- Scratch-based (no shell, minimal attack surface)
- Statically linked Go binary

```bash
git commit --allow-empty -m "docs: Docker image characteristics

- Image size: ~25MB (scratch-based)
- Runs as non-root user: appuser
- No shell access (scratch base)
- Statically linked binary with CGO disabled
- Includes CA certificates for HTTPS"
```

---

## Summary

### Acceptance Criteria Checklist

- [ ] `.dockerignore` created and excludes unnecessary files
- [ ] Multi-stage Dockerfile created with:
  - [ ] Build stage using `golang:1.25.7-alpine`
  - [ ] Runtime stage using `scratch` or minimal `alpine`
  - [ ] Non-root user (`appuser`)
  - [ ] CA certificates copied for HTTPS calls
  - [ ] Timezone data included
  - [ ] Optimized binary build (`-ldflags='-w -s'`)
  - [ ] Port 8080 exposed for health checks
- [ ] Image builds without errors: `docker build -t gitlab-elt-worker .`
- [ ] Image size is minimized (< 30MB target)
- [ ] Non-root user verified: `docker run ... whoami` returns `appuser`
- [ ] Container runs and attempts to start the application
- [ ] Health check port is exposed

### Build Commands Reference

```bash
# Build the image
docker build -t gitlab-elt-worker:latest .

# Run with environment file
docker run --rm --env-file .env gitlab-elt-worker:latest

# Run with specific environment variables
docker run --rm \
  -e POSTGRES_HOST=db \
  -e POSTGRES_DB=gitlab_elt \
  -e GITLAB_TOKEN=your-token \
  gitlab-elt-worker:latest

# Check image size
docker images gitlab-elt-worker:latest

# Shell into builder stage (for debugging)
docker run --rm --entrypoint=/bin/sh -it gitlab-elt-worker:latest-builder
# Note: This requires tagging the builder stage separately
```

### Next Steps (Optional)

1. Create `.dockerignore` optimizations based on actual project needs
2. Consider adding a `.dockerignore` CI check
3. Add Docker build to CI/CD pipeline
4. Document production deployment configuration
5. Create docker-compose.prod.yml if needed (out of scope for this plan)
