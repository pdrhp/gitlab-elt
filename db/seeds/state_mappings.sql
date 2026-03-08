-- Seed data: Mapeamento de labels baseado em dados reais dos projetos GitLab
-- Análise feita em: 2025-02-28
-- Projetos analisados: dflegal-expo, EDFLegalAdmin, ProntobookWeb, VisaWeb, vigDigital-expo

-- =============================================================================
-- STATE MAPPING - Labels de Workflow (Colunas de Board)
-- =============================================================================

-- BACKLOG: Tarefas identificadas, não priorizadas ou prontas para iniciar
INSERT INTO state_mapping (gitlab_label_name, canonical_state, description) VALUES
('Backlog', 'BACKLOG', 'Tarefas identificadas, não priorizadas'),
('Não iniciado', 'BACKLOG', 'Tarefas priorizadas, prontas para iniciar'),
('To Do', 'BACKLOG', 'Tarefa a fazer'),
(' backlog', 'BACKLOG', 'Label com espaço - variação comum');

-- IN_PROGRESS: Tarefas em desenvolvimento ativo
INSERT INTO state_mapping (gitlab_label_name, canonical_state, description) VALUES
('Em dev', 'IN_PROGRESS', 'Tarefas em desenvolvimento ativo'),
('Em Andamento', 'IN_PROGRESS', 'Tarefas sendo trabalhadas no momento'),
('Doing', 'IN_PROGRESS', 'Em execução'),
('Development', 'IN_PROGRESS', 'Fase de desenvolvimento'),
('In Progress', 'IN_PROGRESS', 'Em progresso'),
('Desenvolvimento', 'IN_PROGRESS', 'Em desenvolvimento');

-- QA_REVIEW: Teste, revisão, correção e aguardando publicação
-- Todas as variações de QA e testes vão para o mesmo estado canônico
INSERT INTO state_mapping (gitlab_label_name, canonical_state, description) VALUES
('Teste HOM', 'QA_REVIEW', 'Teste no ambiente de homologação'),
('Teste Prod', 'QA_REVIEW', 'Teste no ambiente de produção'),
('Não Publicado', 'QA_REVIEW', 'Tarefas revisadas, aguardando publicação/homologação'),
('Precisa de Correção', 'QA_REVIEW', 'Tarefas testadas com problemas, rejeitadas pelo QA'),
('TEST/REVIEW', 'QA_REVIEW', 'Em teste ou revisão'),
('QA', 'QA_REVIEW', 'Quality Assurance'),
('Review', 'QA_REVIEW', 'Em revisão de código'),
('Testing', 'QA_REVIEW', 'Em teste'),
('Correção', 'QA_REVIEW', 'Em correção/ajuste (quando usado como estado)');

-- BLOCKED: Tarefas pausadas, bloqueadas ou impedidas
INSERT INTO state_mapping (gitlab_label_name, canonical_state, description) VALUES
('Bloqueado', 'BLOCKED', 'Demanda pausada/bloqueada por algum motivo'),
('Blocked', 'BLOCKED', 'Blocked'),
('Impedido', 'BLOCKED', 'Com impedimento'),
('Aguardando', 'BLOCKED', 'Aguardando dependência ou resposta'),
('On Hold', 'BLOCKED', 'Em espera');

-- DONE: Tarefas finalizadas, testadas e publicadas
INSERT INTO state_mapping (gitlab_label_name, canonical_state, description) VALUES
('Concluído', 'DONE', 'Tarefas finalizadas, testadas e publicadas'),
('Done', 'DONE', 'Concluído'),
('Closed', 'DONE', 'Fechado'),
('Finalizado', 'DONE', 'Finalizado'),
('Completo', 'DONE', 'Completo'),
('Resolvido', 'DONE', 'Resolvido');

-- CANCELED: Tarefas descartadas ou canceladas
INSERT INTO state_mapping (gitlab_label_name, canonical_state, description) VALUES
('Cancelado', 'CANCELED', 'Tarefas descartadas ou canceladas'),
('Cancelled', 'CANCELED', 'Cancelled'),
('Won''t Fix', 'CANCELED', 'Não será corrigido'),
('Invalid', 'CANCELED', 'Inválido'),
('Duplicate', 'CANCELED', 'Duplicado');

-- =============================================================================
-- METADATA MAPPING - Labels de Categoria/Tag (não-estados)
-- =============================================================================

-- TIPO: Tipo da tarefa/issue
INSERT INTO metadata_mapping (gitlab_label_name, metadata_key) VALUES
('Correção', 'tipo'),
('Bug', 'tipo'),
('Melhoria', 'tipo'),
('Enhancement', 'tipo'),
('Feature', 'tipo'),
('Infra', 'tipo'),
('documentação', 'tipo');

-- PRIORIDADE: Nível de prioridade (múltiplas variações encontradas)
INSERT INTO metadata_mapping (gitlab_label_name, metadata_key) VALUES
('PRIORIDADE: URGENTE', 'prioridade'),
('PRIORIDADE: ALTA', 'prioridade'),
('PRIORIDADE: MÉDIO', 'prioridade'),
('PRIORIDADE: MÉDIA', 'prioridade'),
('PRIORIDADE: BAIXA', 'prioridade'),
('P1', 'prioridade'),
('P2', 'prioridade'),
('P3', 'prioridade');

-- ÁREA/STACK: Área técnica ou stack
INSERT INTO metadata_mapping (gitlab_label_name, metadata_key) VALUES
('Backend', 'area'),
('Frontend', 'area'),
('Infra', 'area'),
('DevOps', 'area'),
('UX/UI', 'area');

-- COMPLEXIDADE: Nível de complexidade (encontrado no ProntobookWeb)
INSERT INTO metadata_mapping (gitlab_label_name, metadata_key) VALUES
('NÍVEL 1', 'complexidade'),
('NÍVEL 2', 'complexidade'),
('NÍVEL 3', 'complexidade');

-- STATUS/REVISÃO: Status especiais de revisão
INSERT INTO metadata_mapping (gitlab_label_name, metadata_key) VALUES
('Esperando revisão', 'status_revisao'),
('Teste Automatizado', 'status_teste'),
('feedback do usuário', 'origem');

-- =============================================================================
-- OBSERVAÇÕES IMPORTANTES
-- =============================================================================

-- Nota 1: A label 'Correção' aparece em CATEGORIAS diferentes nos projetos:
--   - dflegal-expo: "Metadado (Categoria/Tag)" - é um tipo de tarefa
--   - EDFLegalAdmin: "Metadado (Categoria/Tag)" - é um tipo de tarefa  
--   - vigDigital-expo: Duas entradas! Uma como "Metadado" e outra também
--   
--   Decisão: Mapear 'Correção' como METADADO (tipo), não como estado.
--   Se for usada como label de board (workflow), o State Mapper retornará UNKNOWN
--   e ela será logada em unknown_labels_log para análise.

-- Nota 2: O label 'Teste HOM' aparece como "Metadado" em vigDigital-expo,
-- mas em todos os outros projetos é "Coluna de Board". 
-- A maioria vence: mapeado como QA_REVIEW.

-- Nota 3: Variações de prioridade encontradas:
--   - PRIORIDADE: ALTA/BAIXA/MÉDIO (dflegal-expo, EDFLegalAdmin, ProntobookWeb)
--   - PRIORIDADE: MÉDIA (VisaWeb - feminino)
--   - P1, P2, P3 (VisaWeb - formato simplificado)
-- Todas são mapeadas para a mesma chave 'prioridade'.
