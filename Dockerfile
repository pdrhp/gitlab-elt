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
