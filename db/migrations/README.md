# Database Migrations - GitLab ELT Worker

## Arquitetura Bronze/Silver

Este diretório contém as migrations do PostgreSQL seguindo a arquitetura Medallion (Bronze/Silver).

### Estrutura das Migrations

#### Bronze Layer (Raw Data) - 000001-000002
Dados brutos extraídos do GitLab, preservados exatamente como recebidos da API.

| Migration | Tabela | Descrição |
|-----------|--------|-----------|
| 000001 | `raw_projects` | Cache de metadados de projetos (payload JSONB) |
| 000002 | `raw_events` | Eventos brutos da API (label_events, notes) em JSONB |

#### Sync Tracking - 000003
Controle de cursor para sincronização incremental.

| Migration | Tabela | Descrição |
|-----------|--------|-----------|
| 000003 | `sync_state` | Cursor `last_synced_at` por projeto para incremental extraction |

#### Silver Layer (Normalized) - 000004-000007
Dados transformados, normalizados e estruturados, prontos para análise.

| Migration | Tabela | Descrição |
|-----------|--------|-----------|
| 000004 | `projects` | Projetos normalizados (a partir de raw_projects) |
| 000005 | `issues` | Issues estruturadas com metadados |
| 000006 | `issue_events` | Eventos mapeados com estados canônicos |
| 000007 | `issue_comments` | Comentários estruturados |

#### Config Layer - 000008-000010
Tabelas de configuração para o State Mapper.

| Migration | Tabela | Descrição |
|-----------|--------|-----------|
| 000008 | `state_mapping` | Mapeamento label → estado canônico |
| 000009 | `metadata_mapping` | Mapeamento label → metadado categórico |
| 000010 | `unknown_labels_log` | Log de labels não mapeadas (auditoria) |

> **Nota:** Views analíticas serão criadas pelo sistema downstream, não por este worker.

#### Optimization Layer - 000011-000015
Tabelas de configuração para o State Mapper.

| Migration | Tabela | Descrição |
|-----------|--------|-----------|
| 000011 | `dead_letter` | Log de eventos falhos (retry queue) |
| 000012 | `raw_issues` | Cache de issues brutos (JSONB) |
| 000013 | (views) | Views da Golden Engineering |
| 000014 | (indexes) | Índices para otimização de ghost-work queries |
| 000015 | `mv_ghost_work_issues` | Materialized view opcional para workloads pesados (fallback se Task 4 falhar) |

**Migration 000014 - Ghost Work Query Optimization**

Adiciona 5 índices seletivos para otimizar consultas de ghost-work:
- `idx_issues_assignees_gin`: GIN index para filtro por assignees (JSONB array)
- `idx_issues_metadata_labels_gin`: GIN index para filtro por metadata_labels (JSONB array)
- `idx_issues_ghost_work_completed`: Índice parcial para issues com `current_canonical_state = 'DONE'` (proxy para ghost-work: issues finalizadas que possivelmente pularam IN_PROGRESS)
- `idx_issue_events_ghost_transitions`: Índice parcial para transições ghost (BACKLOG, DONE, QA_REVIEW)
- `idx_issue_events_project_ghost`: Índice parcial para eventos ghost por projeto

**Nota:** O índice `idx_issues_ghost_work_completed` usa `current_canonical_state = 'DONE'` ao invés de `skipped_in_progress_flag = true` porque este último é uma coluna computada na view `vw_issue_lifecycle_metrics`, não uma coluna física na tabela `issues`.

**Migration 000015 - Optional Materialized View for Ghost Work**

Cria uma materialized view `mv_ghost_work_issues` como fallback para workloads pesados onde a Task 4 (índices seletivos) não foi suficiente para atingir o threshold de performance (p95 > 2s).

**Conteúdo da MV:**
- Pre-computa o join entre `vw_issue_lifecycle_metrics` e `vw_issue_state_transitions`
- Filtra apenas issues com `skipped_in_progress_flag = true`
- Foca em transições BACKLOG → DONE ou BACKLOG → QA_REVIEW
- Inclui 3 índices para otimização: project_id, final_done_at, skipped_in_progress_flag

**Refresh Strategy:**
```sql
-- Manual refresh (runbook)
REFRESH MATERIALIZED VIEW mv_ghost_work_issues;

-- Future: concurrent refresh (requires unique index)
-- REFRESH MATERIALIZED VIEW CONCURRENTLY mv_ghost_work_issues;
```

**Caveat: Por que NÃO usamos `CONCURRENTLY`**

Esta migration intencionalmente **NÃO** usa `CREATE INDEX CONCURRENTLY` porque:
1. O runner de migrations local (`make migrate-up`) executa dentro de transação
2. `CONCURRENTLY` não é permitido dentro de transações no PostgreSQL
3. Para ambiente local/dev, o downtime é aceitável e preferível à complexidade
4. Em produção, use um processo de deploy que suporte migrations não-transacionais ou execute manualmente com `CONCURRENTLY`

## Comandos Úteis

### Aplicar todas as migrations
```bash
export DATABASE_URL="postgres://gitlab_elt:gitlab_elt_dev@localhost:5432/gitlab_elt?sslmode=disable"
migrate -path db/migrations -database "$DATABASE_URL" up
```

### Rollback de todas as migrations
```bash
migrate -path db/migrations -database "$DATABASE_URL" down -all
```

### Criar nova migration
```bash
migrate create -ext sql -dir db/migrations -seq nome_da_migration
```

### Aplicar seeds (mapeamentos iniciais)
```bash
psql $DATABASE_URL -f db/seeds/state_mappings.sql
```

## Convenções de Nomenclatura

- **Número sequencial**: 6 dígitos (000001, 000002, ...)
- **Nome descritivo**: `create_{tabela}_table`
- **Sufixo**: `.up.sql` para criar, `.down.sql` para deletar

## Relacionamentos

```
raw_projects (Bronze) ──┬── sync_state
                        │
projects (Silver) ──────┼── issues ───┬── issue_events
                        │             └── issue_comments
                        │
state_mapping ──────────┘
metadata_mapping ───────┘
```

## Estados Canônicos

Os seguintes estados canônicos são suportados:

- `BACKLOG` - Tarefa no backlog/não iniciada
- `IN_PROGRESS` - Em desenvolvimento/andamento
- `QA_REVIEW` - Em teste/revisão/QA
- `BLOCKED` - Bloqueada/aguardando
- `DONE` - Concluída/finalizada
- `CANCELED` - Cancelada/não será feita
- `UNKNOWN` - Estado não mapeado (fallback)
