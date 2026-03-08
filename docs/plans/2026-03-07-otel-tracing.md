# OTEL Tracing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Emit spans OpenTelemetry em todo o worker (sync/discovery/extract/transform/GitLab) e exportar via OTLP/HTTP para o Alloy (mesmo endpoint das métricas), garantindo contexto propagado e correlação com métricas/logs.

**Architecture:** Reutilizar o builder de resource/config existentes; adicionar TracerProvider + exporter OTLP HTTP, injetar tracer/context no pipeline, instrumentar operações (jobs cron, GitLab requests, persistência SQL). `/metrics` permanece. Documentar env vars e visualização (Tempo/Grafana).

**Tech Stack:** Go, `go.opentelemetry.io/otel/sdk/trace`, `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`, existing config/resource builders, Alloy/Tempo.

---

### Task 1: OTEL tracing config surface

**Files:** `.env.example`, `internal/config/config.go`, `internal/config/config_test.go`

1. **Failing tests**
   - Expand config tests com `TestLoad_OTelTracingDefaults`, `TestLoad_OTelTracingOverrides`, `TestLoad_OTelTracingInvalidHeaders` verificando novos env vars (`OTEL_TRACES_ENDPOINT`, `OTEL_TRACES_HEADERS`, `OTEL_TRACES_INSECURE`, `OTEL_TRACES_SAMPLER`). Defaults: endpoint vazio, headers map vazio, insecure false, sampler `parentbased_always_on`.
   - Rodar `GOROOT=/home/pedrohenrique/.goenv/versions/1.25.7 go test ./internal/config -run OTelTracing -v` (deve falhar).

2. **Implementar parsing**
   - Estender `WorkerOTelConfig` com `Tracing OTelTracingConfig`.
   - Função `loadOTelTracingConfig` reutiliza parsing de headers do metrics helper e valida `OTEL_TRACES_SAMPLER` (suportar `always_on`, `always_off`, `parentbased_always_on`, `traceidratio:<float>`; erro se inválido).
   - Atualizar `.env.example` comentando os novos envs.

3. **Re-testar**
   - `GOROOT=/home/pedrohenrique/.goenv/versions/1.25.7 go test ./internal/config -v`

---

### Task 2: Tracer provider builder + wiring

**Files:** `internal/telemetry/tracing.go` (novo), `internal/telemetry/tracing_test.go` (novo), `cmd/worker/main.go`, `go.mod/go.sum`

1. **Builder tests (fail first)**
   - Criar testes simulando: endpoint vazio → sem provider; endpoint HTTP → exporter funcional; endpoint HTTPS insecure → TLS skip; sampler ratio; propagate headers. Use `httptest` para fake OTLP server.

2. **Implement builder**
   - `func BuildTracerProvider(ctx context.Context, cfg config.OTelTracingConfig, res *resource.Resource, logger *slog.Logger) (*trace.TracerProvider, func(context.Context) error, error)`.
   - Endpoint vazio: return nil, nil, nil.
   - Caso contrário, construir `otlptracehttp.New` (mesma lógica do metrics builder para endpoint vs endpointURL, headers, insecure). Sampler via helper (parse string). Criar `trace.NewTracerProvider(trace.WithSampler(...), trace.WithResource(res), trace.WithBatcher(exporter))`.
   - Retornar shutdown `tp.Shutdown`.

3. **Wire em `cmd/worker/main.go`**
   - Após criar resource (Task 1 prévia), chamar builder e armazenar `tp` + `tracer := tp.Tracer("github.com/pdrhp/gitlab-elt/worker")`. Defer shutdown (timeout 5s) igual OTLP meter.
   - Passar tracer para serviços (discovery, extractor, transformer, GitLab client, etc.) via seus construtores.
   - Garantir fallback (se builder retorna erro, logar e seguir sem tracing).

4. **go.mod/go.sum**
   - Adicionar dependências `go.opentelemetry.io/otel/sdk/trace`, `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`.

5. **Tests**
   - `GOROOT=/home/pedrohenrique/.goenv/versions/1.25.7 go test ./internal/telemetry -v`
   - `go test ./cmd/worker -v` (garantir build).

---

### Task 3: Instrumentação de spans

**Files:**
- `internal/gitlab/client.go` (+ tests)
- `internal/discovery/service.go`
- `internal/sync/extractor.go`
- `internal/transformer/service.go`
- `cmd/worker/main.go` (para envolver sync job)
- `internal/telemetry/tracing.go` (helpers para iniciar spans)

1. **Helper**
   - Criar utilitário `telemetry.StartSpan(ctx, tracer, name, opts...) (context.Context, trace.Span)` que aplica atributos padrão (ex.: serviço, job agendado).

2. **GitLab client**
   - Em cada request, iniciar span `gitlab.request` com atributos HTTP (method, url, status), endpoint lógico (list_projects/list_issues/etc.). Propagar `ctx` para `http.Request` (via `otelhttptrace` se preferir). Testes: usar `oteltest` tracer para garantir spans são criados.

3. **Discovery e Sync**
   - `syncFunc`: criar span raiz `worker.sync` (attributes: schedule, projects processed). Passar `ctx` para extractor/transformer.
   - Discovery: span `worker.discovery` por run, child spans por project persistido.
   - Extractor: spans `worker.extract.project` com subspan para cada issue (`worker.extract.issue`), subspan `worker.raw.persist` para inserts.
   - Transformer: span por batch (`worker.transform.batch`), child spans para `worker.transform.label_event`, `worker.transform.note`, `worker.transform.dlq`.
   - Context propagation: atualize assinaturas dos métodos para aceitar `context.Context` e sempre retornar spans com `span.End()` deferido.

4. **Tests**
   - Unit tests com tracer de memória (`go.opentelemetry.io/otel/sdk/trace/tracetest`) assegurando que os principais métodos criam spans e propagam context (ex.: extractor test gravando spans; GitLab client test verificando attributes). Running `go test ./internal/gitlab ./internal/discovery ./internal/sync ./internal/transformer -run Tracing -v`.

---

### Task 4: Docs & runbook

**Files:** `docs/OBSERVABILITY.md`, `docs/implementation-roadmap.md`

1. **OBSERVABILITY**
   - Adicionar seção “Tracing”: explicar env vars (`OTEL_TRACES_*`), sample de `curl`/Grafana Tempo, como correlacionar com métricas, e checklist (ver spans no Tempo, etc.).

2. **Roadmap**
   - Atualizar Phase 4 → Task 4.1.3 como ✅ (descrever tracing via OTLP, spans por operação, doc atualizada).

3. **Optional snippet**
   - Incluir passo no runbook para validar spans (`tempo-query`, `grafana`).

---

Plan ready. Implementation will follow subagent-driven flow per user request.
