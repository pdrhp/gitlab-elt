# CI/CD + Docker

Este documento descreve o pipeline de CI/CD do projeto, como configurar o GitHub para publicar imagem no Docker Hub e como usar a imagem gerada.

Para o fluxo de backfill efemero, veja tambem `docs/BACKFILL_DOCKER.md`.

## Fluxo do pipeline

Workflow: `.github/workflows/ci-cd.yml`

- `pull_request` para `main`
  - Job `test`: instala Go e roda `make test`
  - Job `build`: valida build com `make build` e confirma binario `bin/worker`
- `push` para `main`
  - Executa `test` + `build`
  - Executa `docker` (buildx + login Docker Hub + build/push)

Workflow manual de backfill: `.github/workflows/backfill-image.yml`

- `workflow_dispatch`
  - Builda `Dockerfile.backfill`
  - Faz push da imagem de backfill no Docker Hub

## Publicacao da imagem Docker

No job `docker`, a imagem e publicada com base em `vars.DOCKERHUB_IMAGE` (formato `usuario/repositorio`) com as tags:

- `latest`
- `sha-<commit>` (exemplo: `sha-a1b2c3...`)

Exemplo de nome final:

- `usuario/repositorio:latest`
- `usuario/repositorio:sha-<commit>`

## Configuracao no GitHub (Repository Secrets e Variables)

No repositorio GitHub, configure:

### Secrets

- `DOCKERHUB_USERNAME`: usuario do Docker Hub
- `DOCKERHUB_TOKEN`: Access Token do Docker Hub

### Variables

- `DOCKERHUB_IMAGE`: nome da imagem no formato `usuario/repositorio`

## Como fazer pull e run da imagem

Pull da ultima imagem:

```bash
docker pull <usuario/repositorio>:latest
```

Run com variaveis minimas:

```bash
docker run --rm -p 8080:8080 \
  -e POSTGRES_HOST=localhost \
  -e POSTGRES_PORT=5432 \
  -e POSTGRES_USER=gitlab_elt \
  -e POSTGRES_PASSWORD=gitlab_elt_dev \
  -e POSTGRES_DB=gitlab_elt \
  -e GITLAB_BASE_URL=https://gitlab.com \
  -e GITLAB_TOKEN=<seu-token> \
  -e GITLAB_PROJECT_IDS=<id1,id2> \
  <usuario/repositorio>:latest
```

Observacao: ao inves de `GITLAB_PROJECT_IDS`, voce pode usar `GITLAB_GROUP_IDS`. Pelo menos um dos dois deve estar definido.

## Variaveis de ambiente da aplicacao (defaults)

Referencias: `.env.example` e `internal/config/config.go`.

Obrigatorias (sem default):

- `POSTGRES_HOST`
- `POSTGRES_PORT`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_DB`
- `GITLAB_BASE_URL`
- `GITLAB_TOKEN`
- `GITLAB_PROJECT_IDS` ou `GITLAB_GROUP_IDS` (ao menos um)

Opcionais com default:

- `HEALTH_PORT=8080`
- `GITLAB_RATE_LIMIT=20`
- `GITLAB_RETRY_MAX=3`
- `SCHEDULER_SYNC_PEAK=*/15 * * * *`
- `SCHEDULER_SYNC_OFFPEAK=0 */4 * * *`
- `SCHEDULER_DISCOVERY=0 */6 * * *`
- `OTEL_TRACES_SAMPLER=parentbased_always_on`

Opcionais sem default funcional (vazio desabilita ou nao adiciona):

- `OTEL_METRICS_ENDPOINT`
- `OTEL_METRICS_HEADERS`
- `OTEL_METRICS_INSECURE=false`
- `OTEL_TRACES_ENDPOINT`
- `OTEL_TRACES_HEADERS`
- `OTEL_TRACES_INSECURE=false`
- `OTEL_SERVICE_NAME=gitlab-elt-worker` (default interno)
- `OTEL_SERVICE_VERSION=`
- `OTEL_ENVIRONMENT=production` (default interno)
- `OTEL_SERVICE_NAMESPACE=`
- `OTEL_SERVICE_INSTANCE_ID=`
- `OTEL_RESOURCE_ATTRIBUTES=`

## Troubleshooting rapido

- Erro `vars.DOCKERHUB_IMAGE nao foi definida`:
  - Configure `DOCKERHUB_IMAGE` em Settings > Secrets and variables > Actions > Variables.
- Falha no login Docker Hub:
  - Verifique `DOCKERHUB_USERNAME` e `DOCKERHUB_TOKEN` (token valido, sem expiracao).
- Falha de build Docker:
  - Confirme que o `Dockerfile` existe na raiz e builda localmente com `docker build -t teste-local .`.
- Falha nos testes no CI e nao local:
  - Compare versao do Go local com a do `go.mod`.
  - Rode `make test` localmente antes de abrir PR.
