.PHONY: help build run test lint clean docker-up docker-down migrate-up migrate-down migrate-create sqlc-generate seed health reset-db test-observability

# Load .env if it exists
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

GO ?= go
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
HEALTH_PORT ?= 8080
PG_CLI_IMAGE ?= postgres:16-alpine

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' Makefile | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the worker binary
	go build -o bin/worker cmd/worker/main.go

run: ## Run the worker locally
	go run cmd/worker/main.go

test: ## Run all tests
	go test ./... -v -race

test-observability: ## Run observability integration suite (set GOROOT externally if needed) with metrics registry exposure checks
	$(GO) test -tags integration ./internal/metrics ./internal/health ./internal/gitlab ./internal/sync ./internal/transformer ./tests/integration -v

clean: ## Remove build artifacts
	rm -rf bin/

docker-up: ## Start local infrastructure (PostgreSQL)
	docker compose up -d

docker-down: ## Stop local infrastructure
	docker compose down

migrate-up: ## Run all database migrations
	migrate -path db/migrations -database "$(DATABASE_URL)" -verbose up

migrate-down: ## Rollback all database migrations
	migrate -path db/migrations -database "$(DATABASE_URL)" -verbose down -all

migrate-create: ## Create a new migration (usage: make migrate-create name=create_foo_table)
	migrate create -ext sql -dir db/migrations -seq $(name)

sqlc-generate: ## Generate Go code from SQL queries
	sqlc generate

setup: docker-up migrate-up sqlc-generate build ## Full local setup: start DB, run migrations, generate code, build

seed: ## Apply seed data (state and metadata mappings)
	@command -v psql >/dev/null || (echo "psql não encontrado. Instale o cliente PostgreSQL."; exit 1)
	PGPASSWORD='$(POSTGRES_PASSWORD)' \
	psql -h $(POSTGRES_HOST) -p $(POSTGRES_PORT) -U $(POSTGRES_USER) -d $(POSTGRES_DB) \
		-v ON_ERROR_STOP=1 \
		-f db/seeds/state_mappings.sql

health: ## Check worker health endpoint
	@curl -sf http://localhost:$(HEALTH_PORT)/health | python3 -m json.tool || echo "Health check failed (is the worker running?)"

reset-db: docker-down docker-up ## Reset database (destroy and recreate)
	@echo "Waiting for PostgreSQL to be ready..."
	@sleep 3
	@$(MAKE) migrate-up
	@$(MAKE) seed
	@echo "Database reset complete."
