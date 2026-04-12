# Assignee Cycle Time Guide v3.0

## Visão Geral

Este sistema rastreia tempo de cycle time **por assignee durante seus períodos reais de responsabilidade**, fornecendo métricas individuais JUSTAS.

---

## Problema Resolvido

### Abordagem Anterior (primary_author v2.0) - INJUSTA

```
Issue #23: "Consulta de Infrações por Equipamento"

Métrica v2.0 (primary_author):
┌─────────────────┬──────────────┐
│ primary_author  | cycle_hours  │
├─────────────────┼──────────────┤
│ eduardo_frois   | 4829h        │ ← Tudo atribuído a ele!
│ fabio           | 1539h        │
│ torezan         | 1435h        │
│ nevez           | 133h         │
│ ...             | ...          │
└─────────────────┴──────────────┘
```

**Problema:** O `primary_author` (eduardo_frois) recebeu crédito por 4829 horas, mas na realidade a issue teve **24 períodos de assignee diferentes**, cada um contribuindo parte do trabalho.

### Nova Abordagem (assignee_cycle_time v3.0) - JUSTA

```
Issue #23: "Consulta de Infrações por Equipamento"

Métrica v3.0 (assignee_cycle_time):
┌─────────────────┬──────────────┬──────────────┬──────────────┐
│ assignee        | active_hours | total_hours  | active_pct   │
├─────────────────┼──────────────┼──────────────┼──────────────┤
│ eduardo_frois   | 4829h        | 4829h        | 100%         │
│ nevez           | 2018h        | 2152h        | 94%          │
│ torezan         | 2350h        | 2526h        | 93%          │
│ maria_dev       | 1731h        | 1731h        | 100%         │
│ fabio           | 1539h        | 1539h        | 100%         │
│ riquerbrito     | 1776h        | 1776h        | 100%         │
│ ludwin          | 336h         | 1276h        | 26%          │
│ cleslley        | 383h         | 1901h        | 20%          │
│ nevez           | 307h         | 307h         | 100%         │
│ quiasz          | 98h          | 112h         | 87%          │
│ ... (14 mais)   | ...          | ...          | ...          │
└─────────────────┴──────────────┴──────────────┴──────────────┘
```

**Vantagem:** Cada assignee recebe crédito **APENAS pelo tempo que REALMENTE teve a issue**.

---

## Conceitos Chave

| Conceito | Definição | Como é Calculado |
|----------|-----------|------------------|
| **Assignment Period** | Período entre `assigned_at` e `unassigned_at` | Do JSONB `assignees.history[]` |
| **Active Work** | Tempo em `IN_PROGRESS` + `QA_REVIEW` | Soma das horas nestes estados durante o período |
| **Wait Time** | Tempo em `BACKLOG` + `BLOCKED` | Tempo restante do período |
| **Active Work %** | `(active_cycle_hours / total_hours_as_assignee) * 100` | Eficiência do assignee |

---

## Views Disponíveis

### vw_assignee_cycle_time

Breakdown de tempo por assignee para cada issue.

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `issue_id` | INTEGER | ID interno da issue |
| `issue_iid` | INTEGER | IID da issue |
| `project_id` | INTEGER | ID do projeto |
| `assignee_username` | TEXT | Username do assignee |
| `active_cycle_hours` | NUMERIC | Horas em IN_PROGRESS + QA_REVIEW |
| `in_progress_hours` | NUMERIC | Horas em IN_PROGRESS |
| `qa_review_hours` | NUMERIC | Horas em QA_REVIEW |
| `blocked_hours` | NUMERIC | Horas em BLOCKED |
| `backlog_hours` | NUMERIC | Horas em BACKLOG |
| `total_hours_as_assignee` | NUMERIC | Total de horas como assignee |
| `contributed_active_work` | BOOLEAN | TRUE se trabalhou ativamente |

**Exemplo de uso:**

```sql
-- Quem contribuiu para a issue #23?
SELECT 
    assignee_username,
    active_cycle_hours,
    in_progress_hours,
    qa_review_hours,
    total_hours_as_assignee,
    active_work_pct
FROM vw_assignee_cycle_time
WHERE issue_iid = 23
ORDER BY active_cycle_hours DESC;
```

---

### vw_individual_performance_metrics

Métricas agregadas de performance por assignee.

| Coluna | Tipo | Descrição |
|--------|------|-----------|
| `assignee_username` | TEXT | Username |
| `project_id` | INTEGER | ID do projeto |
| `issues_assigned` | BIGINT | Total de issues atribuídas |
| `issues_contributed` | BIGINT | Issues com trabalho ativo |
| `total_active_cycle_hours` | NUMERIC | Total de horas ativas |
| `avg_active_cycle_per_issue` | NUMERIC | Média de horas ativas por issue |
| `total_dev_hours` | NUMERIC | Total em IN_PROGRESS |
| `total_qa_hours` | NUMERIC | Total em QA_REVIEW |
| `total_blocked_hours` | NUMERIC | Total em BLOCKED |
| `active_work_pct` | NUMERIC | % de tempo ativo |
| `p50_active_cycle_hours` | NUMERIC | Mediana de horas ativas |
| `p95_active_cycle_hours` | NUMERIC | 95º percentil (outliers) |

**Exemplo de uso:**

```sql
-- Top performers do projeto
SELECT 
    assignee_username,
    issues_contributed,
    total_active_cycle_hours,
    avg_active_cycle_per_issue,
    active_work_pct
FROM vw_individual_performance_metrics
WHERE project_id = 123
ORDER BY total_active_cycle_hours DESC
LIMIT 10;
```

---

## Queries Analíticas Prontas

### 1. Ranking de Performers

```sql
SELECT 
    assignee_username,
    issues_contributed,
    total_active_cycle_hours,
    p95_active_cycle_hours,
    active_work_pct
FROM vw_individual_performance_metrics
WHERE project_id = 123
  AND issues_contributed >= 5  -- Mínimo 5 issues
ORDER BY total_active_cycle_hours DESC;
```

### 2. Identificar Gargalos (Muito Tempo Bloqueado)

```sql
SELECT 
    assignee_username,
    issues_assigned,
    total_blocked_hours,
    ROUND((100.0 * total_blocked_hours / total_hours_as_assignee)::numeric, 2) AS blocked_pct
FROM vw_individual_performance_metrics
WHERE total_blocked_hours > 50
ORDER BY blocked_pct DESC;
```

### 3. Evolução de Performance por Mês

```sql
SELECT 
    DATE_TRUNC('month', ie.event_timestamp)::date AS month,
    COUNT(DISTINCT ac.issue_id) AS issues_completed,
    SUM(ac.active_cycle_hours) AS total_active_hours
FROM vw_assignee_cycle_time ac
JOIN issue_events ie ON ie.issue_id = ac.issue_id 
  AND ie.mapped_canonical_state = 'DONE'
WHERE ac.assignee_username = 'nevez'
GROUP BY DATE_TRUNC('month', ie.event_timestamp)
ORDER BY month DESC;
```

### 4. Comparar v2.0 (primary_author) vs v3.0 (assignee_cycle_time)

```sql
SELECT 
    issue_iid,
    primary_author,  -- v2.0
    current_assignee,
    SUM(ac.active_cycle_hours) AS total_assignee_hours,  -- v3.0
    v.cycle_time_hours AS lifecycle_hours
FROM vw_issue_lifecycle_metrics v
JOIN issues i ON i.id = v.issue_id
LEFT JOIN vw_assignee_cycle_time ac ON ac.issue_id = v.issue_id
GROUP BY issue_iid, primary_author, current_assignee, v.cycle_time_hours;
```

---

## Interpretação de Métricas

### Active Work %

| Faixa | Interpretação | Ação |
|-------|---------------|------|
| **>80%** | Assignee trabalha ativamente ✅ | Manter |
| **50-80%** | Mix de trabalho e espera ⚡ | Investigar bloqueios |
| **<50%** | Muito tempo em espera ⚠️ | Revisar processo |

### High Cycle Time Issues

Issues com `active_cycle_hours > 100` podem indicar:
- Complexidade alta
- Bloqueios não resolvidos
- Necessidade de mais recursos

### p95 Active Cycle Hours

- **Baixo (< 20h):** Performer consistente
- **Alto (> 100h):** Variações grandes (algumas issues muito lentas)

---

## Migration

Para aplicar as assignee cycle time metrics:

```bash
export DATABASE_URL="postgres://gitlab_elt:gitlab_elt_dev@localhost:5432/gitlab_elt?sslmode=disable"
migrate -path db/migrations -database "$DATABASE_URL" up
```

### Rollback

```bash
migrate -path db/migrations -database "$DATABASE_URL" down 1
```

**Nota:** v3.0 é **MAIS JUSTO** que v2.0. Rollback não recomendado.

---

## Go API

Queries sqlc disponíveis em `internal/repository/assignee_performance.sql.go`:

```go
// Get cycle time breakdown for an issue
metrics, err := queries.GetAssigneeCycleTimeByIssue(ctx, issueID)

// Get individual performance for a project
perf, err := queries.GetIndividualPerformanceMetrics(ctx, projectID)

// Get top performers
top, err := queries.GetHighPerformers(ctx, projectID)

// Track performance over time
trends, err := queries.GetUserPerformanceOverTime(ctx, projectID, username)
```

---

## Próximos Passos Sugeridos

1. **Dashboard de Performance Individual** - Substituir `primary_author` por `assignee_cycle_time`
2. **Alertas de Blocked Time** - Notificar quando `active_work_pct < 50%`
3. **Trend Analysis** - Acompanhar evolução de `active_work_pct` por usuário
4. **Team Health Metrics** - Agregar por squad/equipe

---

*Documento criado em: 2026-04-11*  
*Migration: 000018_assignee_cycle_time*  
*Versão do Contrato: 3.0*
