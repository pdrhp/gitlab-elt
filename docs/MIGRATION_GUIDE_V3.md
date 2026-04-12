# Migration Guide: Individual Performance v3.0

## Resumo

**Data:** 2026-04-11  
**Migration:** `000017_assignee_cycle_time`  
**Impact:** Implementa métricas JUSTAS de performance individual  
**Ação Requerida:** N/A (primeira versão deste sistema)

---

## Por Que Esta Migration?

### Abordagem Anterior (INJUSTA)

Sistemas de performance tradicionais atribuem TODO o cycle time ao assignee atual ou ao `primary_author`:

```
Issue #23 com 24 assignees diferentes:
- eduardo_frois: 4829h (foi primary_author) ← Recebeu crédito por TUDO
- Outros 23 assignees: 0h ← Parece que não fizeram nada
```

### Nova Abordagem (JUSTA) - v3.0

Cada assignee recebe crédito APENAS pelo tempo que REALMENTE teve a issue:

```
Issue #23 com 24 assignees diferentes:
- eduardo_frois: 4829h (seu tempo real)
- nevez: 2018h (seu tempo real)
- torezan: 2350h (seu tempo real)
- maria_dev: 1731h (seu tempo real)
- ... (cada um com seu tempo real)
```

---

## Comparação de Abordagens

| Cenário | Abordagem Tradicional | v3.0 (assignee_cycle_time) |
|---------|----------------------|---------------------------|
| Issue com 1 assignee | ✅ Correto | ✅ Correto |
| Issue com 3 assignees | ❌ 100% crédito para 1 pessoa | ✅ Crédito dividido justamente |
| Assignee entrou no final | ❌ Parece que fez tudo | ✅ Recebe só suas horas |
| Handoffs frequentes | ❌ Métricas distorcidas | ✅ Cada um com seu tempo |
| Assignee com 20% active_work | ❌ Não detectável | ✅ Visível no `active_work_pct` |

---

## Queries Exemplo

### Dashboard de Performance Individual

```sql
SELECT 
    assignee_username as performer,
    COUNT(DISTINCT issue_id) as issues,
    SUM(active_cycle_hours) as total_active_hours,
    AVG(active_cycle_hours) as avg_active_hours,
    active_work_pct
FROM vw_assignee_cycle_time
GROUP BY assignee_username;
```

---

### Top Performers Report

```sql
SELECT 
    assignee_username,
    issues_contributed,
    total_active_cycle_hours,
    active_work_pct,
    p95_active_cycle_hours
FROM vw_individual_performance_metrics
ORDER BY total_active_cycle_hours DESC
LIMIT 10;
```

---

### Identificar Gargalos

```sql
-- Assignees com muito tempo bloqueado
SELECT 
    assignee_username,
    total_blocked_hours,
    ROUND((100.0 * total_blocked_hours / total_hours_as_assignee)::numeric, 2) AS blocked_pct
FROM vw_individual_performance_metrics
WHERE total_blocked_hours > 50
ORDER BY blocked_pct DESC;

-- Assignees com baixa eficiência (muito wait time)
SELECT 
    assignee_username,
    active_work_pct,
    issues_assigned
FROM vw_individual_performance_metrics
WHERE active_work_pct < 50
  AND issues_assigned >= 5
ORDER BY active_work_pct ASC;
```

---

### Evolução de Performance

```sql
SELECT 
    ac.assignee_username,
    DATE_TRUNC('month', ie.event_timestamp)::date AS month,
    COUNT(DISTINCT ac.issue_id) AS issues_completed,
    SUM(ac.active_cycle_hours) AS total_active_hours
FROM vw_assignee_cycle_time ac
JOIN issue_events ie ON ie.issue_id = ac.issue_id 
  AND ie.mapped_canonical_state = 'DONE'
WHERE ac.assignee_username = 'nevez'
GROUP BY ac.assignee_username, DATE_TRUNC('month', ie.event_timestamp)
ORDER BY month DESC;
```

---

## Breaking Changes

### ⚠️ Importante

Esta é a **primeira versão** do sistema de assignee cycle time. Não há queries anteriores para migrar.

Se você estava usando abordagens alternativas (como `primary_author` de sistemas legados), substitua por:

```sql
-- USE ESTE
SELECT assignee_username, SUM(active_cycle_hours)
FROM vw_assignee_cycle_time
GROUP BY assignee_username;

-- NÃO USE ESTE (legado)
SELECT primary_author, SUM(cycle_time_hours)
FROM vw_issue_lifecycle_metrics
GROUP BY primary_author;
```

---

## Rollback (se necessário)

```bash
export DATABASE_URL="postgres://gitlab_elt:gitlab_elt_dev@localhost:5432/gitlab_elt?sslmode=disable"
migrate -path db/migrations -database "$DATABASE_URL" down 1
```

**⚠️ Nota:** Rollback **NÃO RECOMENDADO** - estas métricas são muito mais justas que abordagens alternativas.

---

## FAQ

### P: Como funciona o cálculo de `active_work_pct`?

**R:** É a porcentagem do tempo que o assignee passou em estados ativos (IN_PROGRESS + QA_REVIEW) vs tempo total como assignee.

```
active_work_pct = (active_cycle_hours / total_hours_as_assignee) * 100
```

### P: Por que minha métrica de "top performer" mostra valores diferentes de outros sistemas?

**R:** Nosso sistema atribui crédito APENAS pelo tempo real de cada assignee. Se outro sistema atribui todo o cycle time a uma pessoa, seus números serão diferentes (e menos justos).

### P: Como lidar com assignees que têm `active_work_pct < 50%`?

**R:** Isso indica que a pessoa passou mais tempo em BACKLOG/BLOCKED do que trabalhando ativamente. Pode ser:
- Issue ficou atribuída mas não foi tocada
- Assignee era formal, mas outra pessoa executou
- Bloqueios externos

Investigue caso a caso.

---

## Próximos Passos

1. **Criar dashboards de performance** usando `vw_individual_performance_metrics`
2. **Adicionar monitoramento de `active_work_pct`** para identificar gargalos
3. **Implementar trend analysis** por mês/usuário
4. **Comunicar mudança** para stakeholders

---

## Suporte

**Documentação Completa:** `docs/ASSIGNEE_CYCLE_TIME_GUIDE.md`  
**Análise Técnica:** `docs/analysis/assignee-authorship-metrics-analysis.md`

**Contato:** Time de Plataforma - Canal #data-engineering

---

*Documento criado em: 2026-04-11*  
*Migration: 000018_assignee_cycle_time*  
*Versão do Contrato: 3.0*
