# Plano de Implementação: GitLab ELT Worker

> **Baseado no PRD:** `docs/prd-gitlab-elt-worker.md` ✅ **v1.0 REVISADO**  
> **Total Estimado:** 9 semanas (com buffer)  
> **Status:** **APROVADO** - Pronto para execução  
> **Prioridade:** Entregar Phase 1-2 em 4 semanas para MVP

---

## Resumo Executivo

O projeto será implementado em **5 fases**, começando pela fundação (infraestrutura) e evoluindo para o core ELT, normalização de dados, observabilidade e deploy em produção.

**Princípios:**
- **Testes Híbridos:** TDD para lógica complexa, integração para fluxos
- **DRY:** Não repetir código
- **YAGNI:** Não construir o que não é necessário agora
- **Frequent Commits:** Commits pequenos e descritivos
- **Code Review:** Cada fase passa por revisão antes da próxima

---

## Estratégia de Testes

> **Por que não TDD em tudo?** ELT é principalmente orquestração (chama API → guarda no banco). TDD puro teria alto custo de mocks para pouco benefício.

### Abordagem Híbrida

| Componente | Estratégia | Justificativa |
|------------|------------|---------------|
| **State Mapper** | TDD ✅ | Lógica pura (label → estado), sem side effects |
| **Edge Cases** | TDD ✅ | Lógica complexa de filtragem, regras claras |
| **Config Loading** | Testes pós-implementação | Orquestração simples, parsing |
| **GitLab Client** | Integração + mocks mínimos | HTTP é externalidade, melhor testar contra real |
| **Sync/Discovery** | Testes de integração E2E | Fluxo completo GitLab → Bronze → Silver |
| **Transformação** | Validação de dados | Verificar se Silver está correto no banco |

### Pirâmide de Testes

```
         /\
        /  \  Testes Unitários (TDD)
       /    \ State Mapper, Edge Cases
      /------\
     /        \ Testes de Integração
    /          \ GitLab Client, Sync básico
   /------------\
  /              \ Testes E2E
 /                \ Fluxo completo
/------------------\
```

### Regra Prática

```
Lógica Pura (input → output) = TDD
Orquestração (API + Banco) = Testes de Integração
```

---

## Phase 1: Foundation + Bronze Layer (Semanas 1-2) ✅ **COMPLETO**

**Status:** ✅ **IMPLEMENTADO E TESTADO**

**Objetivo:** Infrastructure e extração de dados brutos (Bronze Layer)

**Bronze Layer:** Dados brutos da API GitLab, preservados em JSONB, imutáveis, append-only

### Sprint 1.1: Setup e Infraestrutura ✅

#### Task 1.1.1: Go Module e Estrutura ✅
**Status:** IMPLEMENTADO

**Estrutura Atual:**
```
cmd/worker/
cmd/backfill/   ✅ Script de backfill
internal/
  config/       ✅ Config loading com validação
  domain/       ✅ Modelos e interfaces
  gitlab/       ✅ Client HTTP com rate limiting
  repository/   ✅ Acesso a dados (sqlc)
  scheduler/    ✅ Agendamento cron
  sync/         ✅ Extração Bronze
  discovery/    ✅ Descoberta de projetos
  transformer/  ✅ Transformação Silver
  mapper/       ✅ State mapper
  health/       ✅ Healthcheck endpoint
db/
  migrations/   ✅ 12 migrations
  query/        ✅ Queries SQL
  seeds/        ✅ Seed data
```

**Critérios de Aceitação:**
- [x] Projeto compila sem erros
- [x] Estrutura segue Standard Go Project Layout
- [x] `go.mod` inicializado com nome correto
- [x] Pastas scheduler/, sync/, discovery/, transformer/ criadas

---

#### Task 1.1.2: Docker Compose PostgreSQL ✅
**Status:** IMPLEMENTADO

**Implementação:**
- `docker-compose.yml` com PostgreSQL 16
- `.env.example` com todas variáveis
- Healthcheck configurado

**Critérios de Aceitação:**
- [x] PostgreSQL rodando na porta 5432
- [x] Healthcheck respondendo
- [x] Conexão de teste bem-sucedida

---

#### Task 1.1.3: Config Loading ✅
**Status:** IMPLEMENTADO

**Implementação:**
- `internal/config/config.go` com structs completas
- Validação de variáveis obrigatórias
- Testes: `internal/config/config_test.go`

**Configurações:**
- PostgreSQL (host, port, user, password, db)
- GitLab (base_url, token, group_ids, project_ids, rate_limit, retry_max)
- Scheduler (sync_peak, sync_offpeak, discovery)
- Worker (health_port)

**Critérios de Aceitação:**
- [x] Todos os testes passam
- [x] Validação de variáveis obrigatórias
- [x] Parse de lista de project IDs (int)
- [x] Default values para cron schedules

---

#### Task 1.1.2: Docker Compose PostgreSQL
**Duração:** 2-3 horas  
**Dependências:** Task 1.1.1

**Passos:**
1. Criar `docker-compose.yml` com PostgreSQL 16
2. Criar `.env.example` com todas as variáveis necessárias
3. Criar `.gitignore` (bin, .env, IDE files)
4. Copiar `.env.example` para `.env`
5. Subir PostgreSQL: `docker compose up -d`
6. Testar conexão: `docker compose exec postgres psql -U gitlab_elt -c "SELECT 1;"`

**Critérios de Aceitação:**
- [ ] PostgreSQL rodando na porta 5432
- [ ] Healthcheck respondendo
- [ ] Conexão de teste bem-sucedida

---

#### Task 1.1.3: Config Loading
**Duração:** 2-3 horas  
**Dependências:** Task 1.1.2

**Estratégia de Testes:** Testes pós-implementação (não TDD)
> Config loading é orquestração simples. Testes unitários básicos para garantir parsing e validação.

**Passos:**
1. Instalar `godotenv`: `go get github.com/joho/godotenv`
2. Criar `internal/config/config.go` com structs:
   - `Config` (Postgres, GitLab, Worker, Scheduler)
   - `PostgresConfig` com método `DSN()`
   - `GitLabConfig` (URL, Token, ProjectIDs)
   - `SchedulerConfig` (cron expressions)
3. Implementar `Load()` com validação
4. Criar `internal/config/config_test.go` (testes simples pós-implementação):
   - TestLoad_FromEnvVars
   - TestLoad_MissingRequiredVar
   - TestPostgresConfig_DSN
5. Rodar testes: `go test ./internal/config/... -v`

**Critérios de Aceitação:**
- [ ] Todos os testes passam
- [ ] Validação de variáveis obrigatórias
- [ ] Parse de lista de project IDs (int)
- [ ] Default values para cron schedules

---

### Sprint 1.2: Banco de Dados e Logging ✅

#### Task 1.2.1: Migrations - Bronze Layer (Raw Data) ✅
**Status:** IMPLEMENTADO - 12 migrations

**Migrations Criadas:**
```
000001_create_raw_projects_table      # Cache de metadados
000002_create_raw_events_table        # Eventos brutos (payload JSONB)
000003_create_sync_state_table        # Cursor de sincronização
000004_create_projects_table          # Silver: projetos normalizados
000005_create_issues_table            # Silver: issues estruturadas
000006_create_issue_events_table      # Silver: eventos mapeados
000007_create_issue_comments_table    # Silver: comentários
000008_create_state_mapping_table     # Config: mapeamento de estados
000009_create_metadata_mapping_table  # Config: mapeamento de metadados
000010_create_unknown_labels_log_table # Config: log de labels desconhecidas
000011_create_dead_letter_queue_table # Dead letter queue para eventos
000012_create_raw_issues_table        # Bronze: metadados das issues
```

**Tabelas Bronze:**
- `raw_projects`: id (PK), name, path, last_synced_at, raw_metadata (JSONB)
- `raw_events`: id (PK), gitlab_event_id, project_id, issue_iid, event_type, raw_payload (JSONB), fetched_at, created_at, processed
- `raw_issues`: id, gitlab_issue_id, project_id, iid, title, description, state, raw_payload (JSONB), created_at, updated_at

**Tabelas Silver:**
- `projects`: id (PK), name, path, last_synced_at
- `issues`: id (PK), gitlab_issue_id, project_id, iid, title, current_canonical_state, metadata_labels (JSONB), assignees (JSONB), gitlab_created_at
- `issue_events`: id (PK), gitlab_event_id, issue_id, project_id, issue_iid, author_name, raw_label_added, raw_label_removed, mapped_canonical_state, event_timestamp, is_noise, cycle_count
- `issue_comments`: id (PK), gitlab_note_id, issue_id, author_name, body, comment_timestamp

**Tabelas Config:**
- `state_mapping`: id, gitlab_label_name, canonical_state, description
- `metadata_mapping`: id, gitlab_label_name, metadata_key
- `unknown_labels_log`: id, label_name, occurrence_count, first_seen_at, last_seen_at
- `dead_letter_queue`: id, raw_event_id, project_id, issue_iid, event_type, error_message, retry_count, failed_at, resolved

**Critérios de Aceitação:**
- [x] Tabelas Bronze criadas com JSONB para payload
- [x] Tabelas Silver normalizadas
- [x] Tabelas Config criadas
- [x] Índices em campos de busca
- [x] Rollback funciona corretamente

---

#### Task 1.2.2: sqlc Setup e Queries ✅
**Status:** IMPLEMENTADO

**Queries Bronze:**
- `raw_projects.sql`: UpsertRawProject, ListRawProjects, UpdateRawProjectLastSynced
- `raw_events.sql`: BulkInsertRawEvent, ListUnprocessedRawEvents, MarkRawEventsProcessedBatch
- `raw_issues.sql`: UpsertRawIssue, GetRawIssueByProjectAndIID, ListRawIssuesByProject

**Queries Silver:**
- `issues.sql`: UpsertIssue, GetIssueByProjectAndIID, UpdateIssueCanonicalState
- `issue_events.sql`: InsertIssueEvent
- `issue_comments.sql`: InsertIssueComment

**Queries Config:**
- `state_mapping.sql`: ListStateMappings, GetStateMappingByLabel, UpsertStateMapping
- `metadata_mapping.sql`: ListMetadataMappings
- `unknown_labels_log.sql`: UpsertUnknownLabel, ListUnknownLabels, CountUnknownLabels

**Critérios de Aceitação:**
- [x] Código Go gerado para todas as tabelas
- [x] Queries suportam bulk insert
- [x] Structs de models com JSON tags
- [x] Build passa

---

#### Task 1.2.3: Logging Estruturado ✅
**Status:** IMPLEMENTADO

**Implementação:**
- Setup de slog com JSONHandler em `cmd/worker/main.go` e `cmd/backfill/main.go`
- Níveis: DEBUG, INFO, WARN, ERROR
- Logs estruturados com campos: timestamp, level, msg, service
- Logs de startup, erros, progresso de sync

**Exemplo de log:**
```json
{"time":"2026-03-02T22:33:32.795Z","level":"INFO","msg":"connected to database"}
{"time":"2026-03-02T22:33:32.800Z","level":"INFO","msg":"mapper: loaded mappings","state_mappings":34,"metadata_mappings":21}
```

**Critérios de Aceitação:**
- [x] Logs em formato JSON
- [x] Campos padronizados (timestamp, level, msg)
- [x] Logs estruturados em todos os componentes

---

#### Task 1.2.4: Healthcheck Endpoint ✅
**Status:** IMPLEMENTADO

**Implementação:**
- `internal/health/handler.go`: HTTP handler para healthcheck
- Endpoint `/health` na porta configurável (default 8080)
- Retorna JSON: `{"status": "healthy", "timestamp": "..."}`
- Verifica conexão com PostgreSQL antes de responder
- Integrado no `cmd/worker/main.go` com graceful shutdown

**Exemplo de resposta:**
```json
{"status": "healthy", "timestamp": "2026-03-02T22:33:32Z", "database": "connected"}
```

**Critérios de Aceitação:**
- [x] Endpoint responde em < 100ms
- [x] Retorna 200 quando tudo OK
- [x] Retorna 503 se PostgreSQL indisponível
- [x] Porta configurável via env var (HEALTH_PORT)

---

#### Task 1.2.5: Makefile e DX ✅
**Status:** IMPLEMENTADO

**Targets Disponíveis:**
- `make help` - Mostra documentação
- `make build` - Compila binários (worker, backfill)
- `make run` - Roda worker local
- `make test` - Roda testes com race detector
- `make clean` - Remove artefatos
- `make docker-up` - Sobe PostgreSQL
- `make docker-down` - Derruba PostgreSQL
- `make migrate-up` - Aplica migrations
- `make migrate-down` - Rollback migrations
- `make migrate-create name=X` - Cria nova migration
- `make sqlc-generate` - Gera código SQL
- `make setup` - Setup completo (docker-up + migrate-up + sqlc-generate + build)
- `make seed` - Aplica seed data
- `make health` - Verifica health endpoint
- `make reset-db` - Reseta database

**Critérios de Aceitação:**
- [x] `make help` funciona
- [x] `make setup` completo sem erros
- [x] Todos os targets documentados
- [x] Backfill script disponível (`cmd/backfill/main.go`)

---

## Phase 2: Bronze Extraction + Silver Transformation (Semanas 3-4) ✅ **COMPLETO**

**Status:** ✅ **IMPLEMENTADO E FUNCIONANDO**

**Objetivo:** 
1. Extrair dados brutos do GitLab → Bronze Layer
2. Transformar e normalizar Bronze → Silver Layer

**Responsabilidade do Worker:** Fazer Bronze → Silver (ELT completo)

### Sprint 2.1: GitLab Client e Rate Limiting ✅

#### Task 2.1.1: GitLab HTTP Client ✅
**Status:** IMPLEMENTADO

**Implementação em `internal/gitlab/client.go`:**
- Struct `Client` com http.Client configurado
- Métodos implementados:
  - `ListProjects(ctx, groupID)` - Lista projetos de grupos
  - `ListIssues(ctx, projectID, updatedAfter)` - Issues modificadas desde
  - `ListLabelEvents(ctx, projectID, issueIID)` - Eventos de label
  - `ListNotes(ctx, projectID, issueIID)` - Comentários
- Structs da API em `internal/domain/models.go`:
  - `GitlabProject`, `GitlabIssue`, `GitlabLabelEvent`, `GitlabNote`
- Retry com backoff exponencial (3 tentativas)
- Rate limiting via token bucket (golang.org/x/time/rate)
- Configurável via env vars (GITLAB_RATE_LIMIT, GITLAB_RETRY_MAX)

**Configurações:**
```go
gitlabClient := gitlab.NewClient(
    cfg.Gitlab.BaseURL,
    cfg.Gitlab.Token,
    gitlab.WithRateLimit(cfg.Gitlab.RateLimit),  // default 20 req/s
    gitlab.WithMaxRetries(cfg.Gitlab.RetryMax),  // default 3
)
```

**Critérios de Aceitação:**
- [x] Consegue listar projetos de grupos GitLab
- [x] Consegue buscar issues com filtro updated_after
- [x] Consegue buscar label events de uma issue
- [x] Retry automático em falhas 5xx
- [x] Rate limiting respeitado

---

#### Task 2.1.2: Testes do GitLab Client ✅
**Status:** IMPLEMENTADO

**Testes em `internal/gitlab/client_test.go`:**
- Testes unitários com `httptest` para retry e rate limiting
- Testes de integração em `internal/sync/integration_test.go`

**Cobertura:**
- Rate limiting (token bucket)
- Retry com backoff exponencial
- Paginação de resultados
- Parsing de respostas JSON

**Critérios de Aceitação:**
- [x] Testes unitários para retry
- [x] Testes para rate limiting
- [x] Cobertura de paginação
- [x] Mock server funciona corretamente

---

### Sprint 2.2: Discovery e Sync Services ✅

#### Task 2.2.1: Discovery Service ✅
**Status:** IMPLEMENTADO

**Implementação em `internal/discovery/service.go`:**
- Struct `Service` com dependências (GitLab client, Repository)
- Método `Run(ctx)`:
  - Lista grupos configurados (GITLAB_GROUP_IDS)
  - Para cada grupo, lista projetos via GitLab API
  - Faz UPSERT na tabela raw_projects (Bronze)
  - Projetos novos iniciam com last_synced_at = '1970-01-01'
- Logs de progresso: "discovery: completed", "discovered: 264", "persisted: 264"

**Integração:**
- Worker roda discovery no startup (go routine)
- Agendamento via cron a cada 6h (SCHEDULER_DISCOVERY)

**Critérios de Aceitação:**
- [x] Descobre projetos novos automaticamente
- [x] Não recria projetos existentes (UPSERT)
- [x] Cursor de sync inicia em 1970 para novos projetos
- [x] Log de quantidade de projetos descobertos

---

#### Task 2.2.2: Sync Service - Bronze Extraction ✅
**Status:** IMPLEMENTADO

**Implementação em `internal/sync/extractor.go`:**
- Struct `Extractor` com dependências (GitLab client, Repository)
- Métodos principais:
  - `ExtractAll(ctx)` - Extrai todos os projetos
  - `ExtractProject(ctx, project)` - Extrai um projeto específico
  - `persistIssueMetadata(ctx, projectID, issue)` - NOVO: Salva metadados em raw_issues
  - `fetchIssueRawEvents(ctx, projectID, issue)` - Busca label events + notes

**Fluxo de Extração:**
1. Lista projetos do Bronze (raw_projects)
2. Para cada projeto:
   - Busca last_synced_at
   - Lista issues updated_after (GitLab API)
   - **NOVO**: Persiste metadados da issue em raw_issues (título, id, data de criação)
   - Para cada issue:
     - Lista label events → INSERT into raw_events (JSONB)
     - Lista notes → INSERT into raw_events (JSONB)
3. Persiste cursor: UPDATE raw_projects.last_synced_at

**Dados Extraídos:**
- Issues com metadados (id, iid, title, state, labels, created_at, updated_at)
- Label events (add/remove labels)
- Notes (comentários, não-system)
- Tudo em formato JSONB bruto (imutável)

**Critérios de Aceitação:**
- [x] Extrai issues modificadas desde last_synced_at
- [x] Insere raw_events com payload JSONB completo
- [x] **NOVO**: Salva metadados em raw_issues (título, gitlab_issue_id, created_at)
- [x] Processa múltiplos projetos sequencialmente
- [x] Respeita rate limits
- [x] Atualiza cursor somente após persistência confirmada

---

#### Task 2.2.3: Sync Service - Silver Transformation ✅
**Status:** IMPLEMENTADO

**Silver Layer:** Transformação Bronze → Silver (normalização, mapeamento, limpeza)

**Implementação em `internal/transformer/service.go`:**
- Struct `Service` com dependências (StateMapper, Repository Silver)
- Métodos principais:
  - `TransformAll(ctx)` - Processa todos os eventos em batches
  - `TransformBatch(ctx)` - Processa batch de 500 eventos
  - `transformLabelEvent(ctx, raw)` - Transforma label event
  - `transformNote(ctx, raw)` - Transforma note (comentário)
  - `ensureIssue(ctx, projectID, issueIID)` - Cria/atualiza issue no Silver

**Fluxo de Transformação:**
1. Lista raw_events não processados (batch de 500)
2. Para cada raw_event:
   - Parse do JSONB payload
   - Label Event:
     - Extrai label adicionada/removida
     - Mapeia para canonical_state (via StateMapper)
     - **FIX**: Só loga unknown labels em eventos "add" (não em "remove")
     - Insere issue_event no Silver com mapped_canonical_state
     - Atualiza current_canonical_state na issue
   - Note:
     - Ignora system notes
     - Insere issue_comment no Silver
3. Marca raw_events como processados
4. **NOVO**: Busca metadados de raw_issues para popular:
   - Title real da issue (não "Issue #X")
   - gitlab_issue_id (ID global do GitLab)
   - gitlab_created_at (data real de criação)

**State Mapper:**
- Implementado em `internal/mapper/mapper.go`
- Cache em memória (thread-safe com RWMutex)
- Normalização de labels: lowercase + remove acentos
- Carrega mappings do banco no startup
- Mapeia labels → 7 estados canônicos: BACKLOG, IN_PROGRESS, QA_REVIEW, BLOCKED, DONE, CANCELED, UNKNOWN

**Critérios de Aceitação:**
- [x] Dados Bronze transformados em Silver
- [x] Estados mapeados corretamente (7 estados canônicos)
- [x] **FIX**: Unknown labels só logados para eventos "add" (remove events ignorados)
- [x] **NOVO**: Issues criadas com título real, gitlab_issue_id e data correta
- [x] Raw events marcados como processados
- [x] Idempotente (rodar 2x não duplica Silver)

---

### Sprint 2.3: Agendamento e Graceful Shutdown ✅

#### Task 2.3.1: Scheduler Inteligente ✅
**Status:** IMPLEMENTADO

**Implementação em `internal/scheduler/scheduler.go`:**
- Struct `Scheduler` com robfig/cron/v3
- Métodos:
  - `New(cfg, syncFunc, discoverFunc, logger)` - Cria scheduler
  - `Start()` - Inicia cron jobs
  - `Stop()` - Graceful shutdown (retorna context para aguardar jobs)

**Schedules Configuráveis:**
- Peak (08h-20h): `*/15 * * * *` (SCHEDULER_SYNC_PEAK)
- Off-peak (20h-08h): `0 */4 * * *` (SCHEDULER_SYNC_OFFPEAK)
- Discovery: `0 */6 * * *` (SCHEDULER_DISCOVERY)

**Integração em `cmd/worker/main.go`:**
- Discovery roda no startup (go routine)
- Sync roda nos horários configurados
- Logs de execução em JSON

**Critérios de Aceitação:**
- [x] Jobs executam nos horários corretos
- [x] Schedule peak/off-peak funciona
- [x] Discovery roda a cada 6h
- [x] Logs de execução

---

#### Task 2.3.2: Graceful Shutdown ✅
**Status:** IMPLEMENTADO

**Implementação em `cmd/worker/main.go`:**
- Captura sinais SIGINT, SIGTERM via `signal.Notify()`
- Chama `sched.Stop()` - retorna context para aguardar jobs
- Aguarda jobs em execução (com timeout de 30s)
- Fecha conexão PostgreSQL (`pool.Close()`)
- Shutdown do health server (com timeout de 5s)
- Log de shutdown completo

**Fluxo:**
```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
sig := <-sigCh

stopCtx := sched.Stop()
select {
case <-stopCtx.Done():
    slog.Info("all cron jobs completed")
case <-time.After(30 * time.Second):
    slog.Warn("shutdown timeout exceeded, forcing exit")
}
```

**Critérios de Aceitação:**
- [x] Responde a SIGINT/SIGTERM
- [x] Aguarda jobs em andamento
- [x] Timeout de 30s respeitado
- [x] Não perde dados (transações completam)

**Passos:**
1. Criar migrations Silver:
   ```
   000004_create_projects_table.up.sql         # Silver: projetos normalizados
   000005_create_issues_table.up.sql           # Silver: issues estruturadas
   000006_create_issue_events_table.up.sql     # Silver: eventos mapeados
   000007_create_issue_comments_table.up.sql   # Silver: comentários
   ```
2. Criar `internal/transformer/service.go`:
   - Struct `Transformer` com dependências (StateMapper, Repository Silver)
   - Método `TransformToSilver(ctx)`:
     - Lista raw_events não processados
     - Para cada raw_event:
       - Parse do JSONB payload
       - Mapear labels → canonical_state
       - Extrair metadados (labels categóricas)
       - UPSERT into projects (Silver)
       - UPSERT into issues (Silver)
       - INSERT into issue_events (Silver) com mapped_canonical_state
       - INSERT into issue_comments (Silver)
       - Marcar raw_event como processado
3. Implementar State Mapper (carrega de state_mapping table)
4. Implementar detecção de edge cases básica
5. Criar testes de integração

**Critérios de Aceitação:**
- [ ] Dados Bronze transformados em Silver
- [ ] Estados mapeados corretamente (7 estados canônicos)
- [ ] Metadados extraídos (labels categóricas)
- [ ] Transações garantem consistência Bronze ↔ Silver
- [ ] Raw events marcados como processados
- [ ] Idempotente (rodar 2x não duplica Silver)

---

### Sprint 2.3: Agendamento e Graceful Shutdown

#### Task 2.3.1: Scheduler Inteligente
**Duração:** 3-4 horas  
**Dependências:** Task 2.2.3

**Passos:**
1. Instalar `robfig/cron/v3`: `go get github.com/robfig/cron/v3`
2. Criar `internal/scheduler/scheduler.go`:
   - Struct `Scheduler` com cron.Cron
   - Método `RegisterSyncJob(schedule string, jobFunc func())`
   - Método `RegisterDiscoveryJob(schedule string, jobFunc func())`
   - Método `Start()`
   - Método `Stop() context.Context` (graceful)
3. Implementar schedule dinâmico baseado em horário:
   - Peak (08h-20h): `*/15 * * * *`
   - Off-peak (20h-08h): `0 */4 * * *`
   - Discovery: `0 */6 * * *`

**Critérios de Aceitação:**
- [ ] Jobs executam nos horários corretos
- [ ] Schedule peak/off-peak funciona
- [ ] Discovery roda a cada 6h
- [ ] Logs de execução

---

#### Task 2.3.2: Graceful Shutdown
**Duração:** 2-3 horas  
**Dependências:** Task 2.3.1

**Passos:**
1. Implementar em `main.go`:
   - Capturar sinais SIGINT, SIGTERM
   - Chamar scheduler.Stop()
   - Aguardar jobs em execução (com timeout de 30s)
   - Fechar conexão PostgreSQL
   - Log de shutdown completo
2. Testar: iniciar worker, triggar sync manual, enviar SIGINT durante execução
3. Verificar que job atual completa antes de sair

**Critérios de Aceitação:**
- [ ] Responde a SIGINT/SIGTERM
- [ ] Aguarda jobs em andamento
- [ ] Timeout de 30s respeitado
- [ ] Não perde dados (transações completam)

---

## Phase 3: Silver Refinement (Semanas 5-6) ✅ **SUBSTANCIALMENTE COMPLETO**

**Status:** ✅ **IMPLEMENTADO - State Mapper, Edge Cases, Metadata e preparação analítica entregues; faltam apenas hardening adicional de qualidade**

**Objetivo:** 
1. Refinar Silver (edge cases, noise filtering, state mapping) 🔄
2. Preparar dados para consumo pelo sistema downstream

**O que está implementado:**
- ✅ State Mapper com normalização de labels
- ✅ Unknown labels logging (corrigido para não logar remove events)
- ✅ raw_issues table para metadados
- ✅ Metadata label extraction (`issues.metadata_labels` populado via transformer)
- ✅ Edge cases (dedo nervoso, falso movimento, ping-pong / cycle_count)
- ✅ Data contract e exemplos de query para downstream
- ✅ Gold views para analytics e catálogo de projetos
- ❌ Data quality validation dedicada (`vw_data_quality_*`)

**Nota:** O plano original deixava views analíticas (Gold) fora do escopo, mas elas foram implementadas para simplificar o consumo downstream e suportar contratos futuros de API.

### Sprint 3.1: Configuração e Mapeamento ✅

#### Task 3.1.1: Migrations - Config Tables ✅
**Status:** IMPLEMENTADO

**Migrations Existentes:**
```
000008_create_state_mapping_table.up.sql      # Mapeamento de estados
000009_create_metadata_mapping_table.up.sql   # Mapeamento de metadados
000010_create_unknown_labels_log_table.up.sql # Log de labels desconhecidas
```

**Seed Data:** `db/seeds/state_mappings.sql`
- 35 mapeamentos de estado (BACKLOG, IN_PROGRESS, QA_REVIEW, BLOCKED, DONE, CANCELED)
- 21 mapeamentos de metadados (tipo, prioridade, área, complexidade)
- Labels normalizados (lowercase, sem acentos)

**Critérios de Aceitação:**
- [x] Tabelas de configuração criadas
- [x] Seed data aplicado
- [x] Índices em gitlab_label_name (UNIQUE)
- [x] View de unknown labels funcional

---

#### Task 3.1.2: State Mapper Engine ✅
**Status:** IMPLEMENTADO COM CORREÇÕES

**Estratégia de Testes:** TDD ✅ (Lógica de negócio complexa)

**Implementação em `internal/mapper/mapper.go`:**

**Funcionalidades:**
- Struct `Mapper` com cache em memória (thread-safe com RWMutex)
- Método `LoadFromDB(ctx, queries)` - Carrega do banco no startup
- Método `MapLabel(label string) (canonicalState string, labelType LabelType)`
- Métodos auxiliares: `IsStateLabel()`, `StateFor()`, `MetadataKey()`

**Label Normalization (NOVO):**
```go
func normalizeLabel(label string) string {
    // Converte para lowercase
    normalized := strings.ToLower(label)
    // Remove acentos/diacriticos (NFKD decomposition)
    t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
    normalized, _, _ = transform.String(t, normalized)
    // Trim whitespace
    return strings.TrimSpace(normalized)
}
```

**Exemplo:**
- "Não Iniciado" → "nao iniciado"
- "Em dev" → "em dev"
- "Teste Prod" → "teste prod"

**Enums:**
```go
const (
    LabelTypeState    LabelType = iota  // Workflow state
    LabelTypeMetadata                    // Categorical metadata
    LabelTypeUnknown                     // Not mapped
)
```

**Estados Canônicos:**
- BACKLOG, IN_PROGRESS, QA_REVIEW, BLOCKED, DONE, CANCELED, UNKNOWN

**Testes em `internal/mapper/mapper_test.go`:**
- ✅ `TestMapLabel_SimpleCases`
- ✅ `TestMapLabel_CaseInsensitive` (agora com normalização)
- ✅ `TestMapLabel_WithAccents` (NOVO: testa remoção de acentos)
- ✅ `TestMapLabel_Unknown`
- ✅ `TestMapLabel_Metadata`
- ✅ `TestLoadMappings`

**Critérios de Aceitação:**
- [x] Todos os testes passam (TDD completo)
- [x] Mapeamentos carregados do banco
- [x] Cache funciona (consulta rápida)
- [x] Thread-safe
- [x] Labels desconhecidas retornam UNKNOWN
- [x] **NOVO**: Normalização de labels (case-insensitive, accent-insensitive)
- [x] Cobertura de testes > 80%

---

#### Task 3.1.3: Unknown Labels Logging ✅
**Status:** IMPLEMENTADO COM CORREÇÃO DE BUG

**Implementação em `internal/transformer/service.go`:**

**Bug Corrigido:**
**Problema:** Eventos de remoção de labels (action = "remove") estavam sendo logados como unknown, mesmo quando a label era mapeada. Isso acontecia porque `determineCanonicalState` retorna "UNKNOWN" quando uma label de estado é removida (pois não sabemos o novo estado), mas o transformer estava logando todas as labels "UNKNOWN".

**Exemplo do Bug:**
- "Em dev" tinha 1,574 eventos de remoção
- Todos foram logados em unknown_labels_log
- Mesmo comportamento para "Teste Prod", "Não Iniciado", etc.

**Solução:**
```go
// Só loga unknown labels para eventos "add" E verifica se label realmente não está mapeada
if canonicalState == "UNKNOWN" && le.Action == "add" {
    if _, labelType := s.mapper.MapLabel(labelAdded); labelType == mapper.LabelTypeUnknown {
        s.queries.UpsertUnknownLabel(ctx, labelAdded)
    }
}
```

**Queries em `db/query/unknown_labels_log.sql`:**
- `UpsertUnknownLabel` - Incrementa contador ou cria novo registro
- `ListUnknownLabels` - Lista todas as labels desconhecidas
- `CountUnknownLabels` - Conta total

**View:** `vw_unknown_labels_summary` mostra ranking de labels desconhecidas.

**Resultado:**
- Antes: 1,500+ entradas falsas por label mapeada
- Depois: Apenas 7 labels realmente desconhecidas

**Critérios de Aceitação:**
- [x] Labels desconhecidas são logadas
- [x] Contador incrementa corretamente
- [x] View mostra ranking de labels desconhecidas
- [x] **FIX**: Remove events de labels mapeadas não são mais logados

---

### Sprint 3.2: Edge Cases e Normalização ✅ **IMPLEMENTADO**

#### Task 3.2.1: Tratamento de Edge Cases ✅
**Status:** IMPLEMENTADO

**Implementado em `internal/transformer/edge_cases.go` e integrado em `internal/transformer/service.go`:**
- **Falso Movimento:** Aglutinar transições entre labels do mesmo estado canônico
  - Ex: "Teste HOM" → "Teste Prod" (ambos QA_REVIEW)
  - Não cria novo evento de estado e evita distorção da timeline
  
- **Dedo Nervoso:** Ignorar transições rápidas (< 15 min)
  - Comparar timestamp com evento anterior
  - Se delta < 15min e mesmo estado, marca `is_noise = true`
  
- **Efeito Ping-Pong:** Contar ciclos IN_PROGRESS ↔ QA_REVIEW
  - Incrementa `cycle_count` a cada retorno de QA_REVIEW → IN_PROGRESS

**Arquivos implementados:**
- `internal/transformer/edge_cases.go`
- `internal/transformer/edge_cases_test.go`

**Resultado atual:**
- `issue_events.is_noise` é populado
- `issue_events.cycle_count` é populado
- Timeline Silver fica mais limpa para cálculo de métricas

**Critérios de Aceitação:**
- [x] Falso movimento detectado e aglutinado
- [x] Dedo nervoso ignorado via `is_noise`
- [x] Ciclo de vida contado corretamente
- [x] Testes cobrem casos reais
- [x] Cobertura de testes implementada para a lógica de edge cases

---

#### Task 3.2.2: Metadata Label Extraction ✅
**Status:** IMPLEMENTADO

**Implementado em `internal/transformer/service.go`:**
1. Durante a transformação de label events:
   - Verifica se label é do tipo METADATA via mapper
   - Extrai `metadata_key` (tipo, prioridade, area, complexidade)
   - Acumula labels sem duplicar valores
   
2. Popular `issues.metadata_labels` com JSON:
   ```json
   {
     "tipo": ["Correção", "Feature"],
     "prioridade": ["ALTA"],
     "area": ["Backend", "Frontend"]
   }
   ```

**Resultado atual:**
- `issues.metadata_labels` é populado durante a transformação
- Queries downstream já podem filtrar por tipo/prioridade com JSONB
- `docs/QUERY_EXAMPLES.md` já documenta consultas por metadata

**Critérios de Aceitação:**
- [x] Labels categóricas extraídas durante transformação
- [x] `metadata_labels` populado com JSON válido
- [x] Assignees extraídos e histórico suportado por system notes quando disponível

---

#### Task 3.2.3: Validação e Qualidade dos Dados Silver 🔄
**Status:** PARCIALMENTE IMPLEMENTADO

**O que já existe:**
- `docs/DATA_CONTRACT.md` documenta o schema Silver
- `docs/QUERY_EXAMPLES.md` documenta queries analíticas e operacionais
- Verificações manuais e consultas de consistência foram usadas para validar cobertura de estado, eventos e projetos
- Golden views foram adicionadas para facilitar validação analítica e consumo downstream

**Ainda desejável:**
1. Queries de validação:
   - Integridade referencial (issue_events.issue_id → issues.id)
   - Cobertura de mapeamento (% eventos mapeados)
   - Consistência de timestamps
   
2. View `vw_data_quality_check`:
   - Eventos por projeto
   - Issues sem eventos
   - Eventos com estado UNKNOWN
   - Última atualização por projeto

3. Testes de performance:
   - JOIN issues + issue_events
   - Filtros por projeto e data
   - Performance < 1s para queries comuns

4. Documentação:
   - Schema Silver para downstream
   - Relacionamentos entre tabelas

**Critérios de Aceitação:**
- [x] Documentação do schema Silver para downstream
- [x] Queries de consumo e análise documentadas
- [ ] Dados Silver validados por suite dedicada de integridade
- [ ] Queries/views dedicadas de qualidade (`vw_data_quality_*`)
- [ ] Performance das queries Silver testada formalmente

**Nota:** Além das tabelas Silver, views analíticas Gold agora existem para lead time, cycle time, blocked time, rework e catálogo de projetos.

---

#### Task 3.2.4: Preparação para Handoff 🔄
**Status:** PARCIALMENTE IMPLEMENTADO

**Entregue:**
1. Documentação `docs/DATA_CONTRACT.md`:
   - ✅ Contrato de dados entre worker e sistema downstream
   - ✅ Schema das tabelas Silver
   - ✅ Campos obrigatórios vs opcionais
   - ✅ Tipos de dados e formatos

2. Exemplos de queries:
   - ✅ Query básica: listar issues de um projeto
   - ✅ Query com joins: issues + seus eventos
   - ✅ Query temporal: eventos em um período
   - ✅ Exemplos analíticos (lead time, cycle time, rework)

3. Assets adicionais:
   - ✅ Seed data de mappings em `db/seeds/state_mappings.sql`
   - ✅ Gold views para facilitar integração downstream
   - ❌ Seed data dedicada para cenários analíticos/handoff

**Critérios de Aceitação:**
- [x] Contrato de dados documentado
- [x] Exemplos de queries funcionais
- [x] Schema validado e estável para consumo atual
- [ ] Seed data dedicada disponível

---

## Phase 4: Observabilidade & Hardening (Semanas 7-8)

**Objetivo:** Sistema monitorável, testado e documentado.
**Status:** 🔄 Bases entregues (métricas expostas + alertas/runbook publicados). Pendências: tracing opcional, testes E2E e Dockerfile.

### Sprint 4.1: Observabilidade

#### Task 4.1.1: Métricas Prometheus ✅
**Status:** IMPLEMENTADO — Registro único em `internal/metrics`, instrumentação de discovery/extractor/transformer e exposição de `/metrics` no mesmo servidor do `/health`.

**Implementação:**
- Counters/histogramas padronizados: `gitlab_elt_sync_runs_total`, `gitlab_elt_sync_duration_seconds`, `gitlab_elt_gitlab_requests_total`, `gitlab_elt_gitlab_request_duration_seconds`, `gitlab_elt_raw_events_ingested_total`, `gitlab_elt_transform_results_total`.
- Gauges `gitlab_elt_last_successful_sync_timestamp` e `gitlab_elt_projects_monitored` atualizados pelos serviços (via helper `SetProjectsMonitored`).
- Servidor HTTP adicionou `promhttp.HandlerFor` em `/metrics`; `/health` continua JSON focado em DB readiness.
- OTLP push opcional: `internal/metrics` expõe `BuildOTLPMeter` + `BuildResource`, lendo variáveis (`OTEL_SERVICE_NAME`, `OTEL_SERVICE_VERSION`, `OTEL_ENVIRONMENT`, `OTEL_SERVICE_NAMESPACE`, `OTEL_SERVICE_INSTANCE_ID`, `OTEL_RESOURCE_ATTRIBUTES`, `OTEL_METRICS_ENDPOINT`, `OTEL_METRICS_HEADERS`, `OTEL_METRICS_INSECURE`) para anexar metadata consistente (service, versão, ambiente, host) em métricas/traces futuros. `.env.example` e `docs/OBSERVABILITY.md` documentam o setup (incluindo bridges Alloy → Mimir/Tempo).
- Documentado em `docs/OBSERVABILITY.md` com checklist, exemplos de curl/Prometheus scrape e seção sobre OTLP push + resource metadata.

**Critérios de Aceitação:**
- [x] Endpoint /metrics responde
- [x] Métricas incrementam corretamente
- [x] Labels funcionam (project_id, stage, endpoint, status)
- [x] Formato Prometheus válido

---

#### Task 4.1.2: Alertas e Alertmanager ✅
**Status:** IMPLEMENTADO — Alertas críticos e runbook publicados.

**Implementação:**
- `docs/alerts/rules.yml` define `GitLabWorkerDown`, `SyncStalled`, `SyncErrorBurst` e `UnknownLabelsDetected` com expressões baseadas nos novos gauges/counters e labels consistentes (`severity`, `team`, `service`, `runbook`).
- Annotations trazem resumo/descrição e apontam para seções específicas do runbook (anchors em `docs/OBSERVABILITY.md`).
- `docs/OBSERVABILITY.md` descreve integração do Alertmanager (rotas, templates, receivers) e inclui checklist de carregamento dos arquivos de regra.

**Critérios de Aceitação:**
- [x] Regras de alerta definidas
- [x] Documentação de setup do Alertmanager
- [x] Exemplos de mensagens de alerta nas annotations

---

#### Task 4.1.3: OpenTelemetry Tracing (Opcional/Bônus) ✅
**Status:** IMPLEMENTADO - Tracing distribuído via OTLP/HTTP com spans hierárquicos

**Implementação:**
- **Configuração:** `OTEL_TRACES_ENDPOINT`, `OTEL_TRACES_HEADERS`, `OTEL_TRACES_INSECURE`, `OTEL_TRACES_SAMPLER` em `internal/config/config.go`
- **Infraestrutura:** `internal/telemetry/tracing.go` com `BuildTracerProvider()` exportando via OTLP/HTTP
- **Instrumentação:** Spans em toda a pipeline ELT:
  - `sync.cycle` (raiz) → `discovery.Run` → `extractor.ExtractAll` → `transformer.TransformAll`
  - GitLab API spans: `gitlab.ListGroupProjects`, `gitlab.ListIssues`, `gitlab.ListLabelEvents`, `gitlab.ListNotes`
  - Atributos ricos: project.id, events.count, batch.size, etc.
- **Integração:** Worker inicializa tracer provider em `cmd/worker/main.go`, shutdown graceful com timeout de 5s
- **Reuso:** Mesmos resource attributes de métricas (service.name, version, environment, namespace, instance ID)

**Documentação:** Seção "OpenTelemetry Tracing (optional)" em `docs/OBSERVABILITY.md` com:
- Configuração de variáveis de ambiente
- Estrutura hierárquica dos spans
- Atributos incluídos por span
- Integração com Grafana Alloy → Tempo

**Critérios de Aceitação:**
- [x] Traces gerados e exportados via OTLP/HTTP
- [x] Spans aninhados hierarquicamente (sync → discovery/extract/transform → API calls)
- [x] Context propagation mantém relação pai-filho entre spans
- [x] Mesmos resource attributes de métricas para correlação
- [x] `/metrics` preservado para backward compatibility
- [x] No-op tracer quando tracing não configurado (zero overhead)
- [x] Documentação completa no runbook

---

### Sprint 4.2: Testes de Integração e Documentação

#### Task 4.2.1: Testes de Integração E2E
**Duração:** 5-6 horas  
**Dependências:** Task 4.1.3

**Estratégia:** Testes de integração focados (não mocks pesados)
> Validar fluxo completo: GitLab real → Bronze → Silver. Preferir testes contra infra real a mocks complexos.

**Passos:**
1. Criar ambiente de teste:
   - PostgreSQL local (docker compose)
   - GitLab de teste/development (token de teste)
   - 2-3 projetos de teste conhecidos
2. Criar `tests/integration/e2e_test.go`:
   - **Cenário 1 - Sync Completo:**
     - Rodar discovery em grupo de teste
     - Verificar que projetos foram para Bronze
     - Verificar que transformação criou registros Silver
   - **Cenário 2 - Sync Incremental:**
     - Primeira execução: sync completo
     - Modificar uma issue no GitLab
     - Segunda execução: apenas delta sincronizado
   - **Cenário 3 - Idempotência:**
     - Rodar sync 2x no mesmo período
     - Verificar que não há duplicatas (Bronze e Silver)
   - **Cenário 4 - Edge Cases Reais:**
     - Issues com muitas transições (#215, #356, #359)
     - Verificar que edge cases foram tratados corretamente
3. Criar `tests/benchmark/`:
   - Benchmark de bulk insert (Bronze)
   - Benchmark de state mapping (lógica pura)
4. Criar script de validação de dados:
   - `scripts/validate_silver_data.sql`: Queries para verificar integridade
   - Comparar counts: Bronze vs Silver
   - Verificar mapeamentos (nenhum estado UNKNOWN inesperado)
5. Rodar suite: `make test-integration`

**Critérios de Aceitação:**
- [ ] Testes E2E passam contra GitLab real
- [ ] Sync completo e incremental funcionam
- [ ] Idempotência garantida (sem duplicatas)
- [ ] Edge cases reais tratados corretamente
- [ ] Benchmarks estabelecem baseline de performance

---

#### Task 4.2.2: Documentação Técnica
**Duração:** 4-5 horas  
**Dependências:** Task 4.2.1

**Passos:**
1. Criar `README.md` completo:
   - Visão geral
   - Setup de desenvolvimento
   - Como rodar local
   - Configuração (env vars)
   - Arquitetura (diagramas)
   - Como adicionar novo mapeamento de label
2. Criar `docs/ARCHITECTURE.md`:
   - Decisões arquiteturais
   - Fluxo de dados detalhado
   - Escolha de tecnologias
3. Criar `docs/RUNBOOK.md`:
   - Troubleshooting comum
   - Como investigar falhas
   - Comandos úteis (make, sql, etc.)
4. Criar `docs/DEPLOY.md`:
   - Como fazer deploy
   - Configuração de produção
   - Healthcheck e monitoring

**Critérios de Aceitação:**
- [ ] README claro e completo
- [ ] Documentação de arquitetura
- [ ] Runbook com troubleshooting
- [ ] Guia de deploy

---

#### Task 4.2.3: Dockerfile e Build
**Duração:** 2-3 horas  
**Dependências:** Task 4.2.2

**Passos:**
1. Criar `Dockerfile` (multi-stage):
   - Stage 1: Build (golang:1.22-alpine)
   - Stage 2: Runtime (alpine:latest ou scratch)
   - Copiar binário e certs
   - Non-root user
2. Criar `.dockerignore`
3. Criar `docker-compose.prod.yml` (opcional)
4. Testar build: `docker build -t gitlab-elt-worker .`
5. Testar run: `docker run --env-file .env gitlab-elt-worker`

**Critérios de Aceitação:**
- [ ] Imagem builda sem erros
- [ ] Multi-stage reduz tamanho
- [ ] Non-root user
- [ ] Funciona com docker run

---

## Phase 5: Production (Semana 9+)

**Objetivo:** Deploy em produção e operação.

### Sprint 5.1: Deploy e Validação

#### Task 5.1.1: Deploy em Staging
**Duração:** 3-4 horas  
**Dependências:** Phase 4 completa

**Passos:**
1. Provisionar infra em staging:
   - PostgreSQL (RDS ou container)
   - VM/Kubernetes para worker
   - Prometheus/Grafana para monitoramento
2. Deploy do worker:
   - Subir container com imagem
   - Configurar env vars de staging
   - Aplicar migrations
   - Verificar healthcheck
3. Seed de mapeamentos iniciais
4. Testar sync com 2-3 projetos reais
5. Validar métricas no Grafana

**Critérios de Aceitação:**
- [ ] Worker rodando em staging
- [ ] Conectado ao PostgreSQL
- [ ] Métricas visíveis no Grafana
- [ ] Logs fluindo

---

#### Task 5.1.2: Backfill de Dados Históricos (se necessário)
**Duração:** 4-6 horas  
**Dependências:** Task 5.1.1

**Passos:**
1. Criar script `cmd/backfill/main.go`:
   - Sincroniza dados históricos (últimos 3 meses)
   - Respeita rate limits (mais conservador)
   - Log de progresso
   - Resume de onde parou em caso de falha
2. Rodar em staging primeiro
3. Validar integridade dos dados
4. Agendar janela de manutenção para produção

**Critérios de Aceitação:**
- [ ] Script de backfill funciona
- [ ] Não sobrecarrega API GitLab
- [ ] Dados históricos consistentes
- [ ] Progresso logado

---

#### Task 5.1.3: Deploy em Produção
**Duração:** 2-3 horas  
**Dependências:** Task 5.1.2

**Passos:**
1. Provisionar infra de produção (similar a staging)
2. Deploy do worker
3. Aplicar migrations
4. Popular state_mappings
5. Verificar healthcheck
6. Monitorar primeiras execuções
7. Ajustar alertas se necessário

**Critérios de Aceitação:**
- [ ] Worker saudável em produção
- [ ] Primeiros eventos processados
- [ ] Sem erros críticos
- [ ] Alertas configurados

---

### Sprint 5.2: Handoff e Treinamento

#### Task 5.2.1: Documentação de Operação
**Duração:** 2-3 horas  
**Dependências:** Task 5.1.3

**Passos:**
1. Criar `docs/OPERATIONS.md`:
   - Como escalar horizontalmente (mais workers)
   - Como adicionar novo projeto ao monitoramento
   - Como investigar falhas de sync
   - Como adicionar novo mapeamento de label
   - Rotinas de manutenção (backup, purge)
2. Criar playbook de incidentes
3. Documentar contatos de emergência

**Critérios de Aceitação:**
- [ ] Documentação de operação clara
- [ ] Playbook de incidentes
- [ ] Checklist de operações diárias

---

#### Task 5.2.2: Treinamento da Equipe
**Duração:** 2-3 horas  
**Dependências:** Task 5.2.1

**Passos:**
1. Preparar apresentação:
   - Visão geral do sistema
   - Como funciona o ELT
   - Como interpretar métricas
   - Como investigar problemas
2. Sessão de treinamento com equipe:
   - Demo do dashboard/métricas
   - Exercício de troubleshooting
   - Q&A
3. Gravar sessão para referência futura

**Critérios de Aceitação:**
- [ ] Equipe treinada
- [ ] Sessão gravada
- [ ] Material de referência disponível

---

## Timeline Visual - Progresso Real

```
Semana 1-2          Semana 3-4          Semana 5-6          Semana 7-8          Semana 9+
[Phase 1] ✅        [Phase 2] ✅        [Phase 3] ✅        [Phase 4] 🔄        [Phase 5] ❌
Foundation          Core ELT            Silver Refinement   Observability       Production
+ Bronze            + Silver            (Parcial)           + Hardening         + Deploy
  ✅ Go Module         ✅ GitLab Client     ✅ State Mapper      ✅ Prometheus (/metrics) ❌ Staging
  ✅ Docker            ✅ Discovery         ✅ Unknown Labels    ✅ Alertas + Runbook  ❌ Produção
  ✅ Config            ✅ Extractor         ✅ Normalization     ✅ Tracing           
  ✅ Migrations        ✅ Transformer       ✅ Metadata Labels   ❌ Testes E2E        
  ✅ Logging           ✅ Scheduler         ✅ Edge Cases        ❌ Dockerfile        
  ✅ Healthcheck       ✅ Backfill Script   🔄 Data Quality                          
  ✅ Makefile                              🔄 Handoff                                
                                           ✅ Gold Views                            
```

**Legenda:**
- ✅ **Completo** - Implementado e testado
- 🔄 **Em Andamento/Parcial** - Parte implementada, parte pendente
- ❌ **Pendente** - Não iniciado

**Destaques recentes da Phase 3:**
- ✅ FIX: unknown_labels_log não loga mais remove events de labels mapeadas
- ✅ NEW: Metadata labels populadas em `issues.metadata_labels`
- ✅ NEW: Edge cases implementados (`is_noise`, falso movimento, `cycle_count`)
- ✅ NEW: Data contract e exemplos de query para downstream
- ✅ NEW: Gold views e `vw_projects_catalog` para analytics e integrações futuras

---

## Próximos Passos Imediatos

**Phase 1 e 2 COMPLETAS - Sistema funcional em produção**

**Status Atual:**
- ✅ Phase 1: Foundation completa (Go module, Docker, config, migrations, logging, healthcheck)
- ✅ Phase 2: Core ELT completo (GitLab client, discovery, extraction, transformation, scheduler)
- ✅ Phase 3: Silver refinement entregue para uso downstream (state mapper, metadata, edge cases, data contract)
- ✅ Gold views e catálogo de projetos adicionados para analytics e integração futura

**Próximas Prioridades:**

1. **Data Quality dedicada** (Task 3.2.3) 🔄
   - Criar views/queries dedicadas de integridade e cobertura
   - Formalizar validação automática de consistência Silver/Gold

2. **Handoff adicional** (Task 3.2.4) 🔄
   - Seed data analítica
   - Eventuais docs extras para consumidores internos

3. **Phase 4: Observabilidade (restante)** 📊
   - Tracing (opcional, mas desejável para diagnósticos profundos)
   - Testes de integração E2E cobrindo métricas
   - Documentação expandida (README/ARCHITECTURE/RUNBOOK/DEPLOY)
   - Dockerfile multi-stage / pipeline de build

**Decisões já tomadas:**
- ✅ Dead letter queue: IMPLEMENTADA (migration 000011)
- ✅ raw_issues table: IMPLEMENTADA (migration 000012) - para metadados das issues
- ✅ Backfill: Script disponível em `cmd/backfill/main.go`
- ✅ Rate limiting: 20 req/s default, 10 req/s para backfill
- ✅ Normalização de labels: IMPLEMENTADA (case-insensitive, accent-insensitive)

---

## Checklist de Progresso

### Phase 1 ✅ COMPLETO
- [x] Task 1.1.1: Go Module e Estrutura
- [x] Task 1.1.2: Docker Compose PostgreSQL
- [x] Task 1.1.3: Config Loading
- [x] Task 1.2.1: Migrations Bronze (12 migrations)
- [x] Task 1.2.2: sqlc Setup (todas as tabelas)
- [x] Task 1.2.3: Logging Estruturado
- [x] Task 1.2.4: Healthcheck Endpoint
- [x] Task 1.2.5: Makefile e DX

### Phase 2 ✅ COMPLETO
- [x] Task 2.1.1: GitLab Client
- [x] Task 2.1.2: Testes GitLab Client
- [x] Task 2.2.1: Discovery Service
- [x] Task 2.2.2: Sync Service - Bronze Extraction
- [x] Task 2.2.3: Sync Service - Silver Transformation
- [x] Task 2.3.1: Scheduler Inteligente
- [x] Task 2.3.2: Graceful Shutdown

### Phase 3 ✅ SUBSTANCIALMENTE COMPLETO
- [x] Task 3.1.1: Migrations Config (state_mapping, metadata_mapping, unknown_labels_log)
- [x] Task 3.1.2: State Mapper Engine (com normalização de labels)
- [x] Task 3.1.3: Unknown Labels Logging (bug de remove events corrigido)
- [x] Task 3.2.1: Edge Cases (dedo nervoso, falso movimento, ping-pong)
- [x] Task 3.2.2: Metadata Label Extraction
- [ ] Task 3.2.3: Validação Dados Silver (suite dedicada de qualidade ainda pendente)
- [ ] Task 3.2.4: Preparação Handoff (parcial: docs e exemplos entregues, seed/hardening pendentes)

### Phase 4 🔄 EM ANDAMENTO
- [x] Task 4.1.1: Métricas Prometheus (`internal/metrics`, `/metrics`, docs/OBSERVABILITY.md)
- [x] Task 4.1.2: Alertas + Runbook (`docs/alerts/rules.yml`, anchors no runbook)
- [x] Task 4.1.3: OpenTelemetry Tracing (`internal/telemetry/tracing.go`, spans hierárquicos, OTLP export)
- [ ] Task 4.2.1: Testes Integração E2E
- [ ] Task 4.2.2: Documentação Técnica (README, ARCHITECTURE, RUNBOOK, DEPLOY)
- [ ] Task 4.2.3: Dockerfile

### Phase 5 ❌ PENDENTE
- [x] Task 5.1.2: Backfill Script (cmd/backfill/main.go) ✅ IMPLEMENTADO
- [ ] Task 5.1.1: Deploy em Staging
- [ ] Task 5.1.3: Deploy em Produção
- [ ] Task 5.2.1: Documentação de Operação
- [ ] Task 5.2.2: Treinamento da Equipe
