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
