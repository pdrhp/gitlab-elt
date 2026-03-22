# Backfill com Docker (execucao efemera)

Este documento descreve como buildar e executar a imagem de backfill para carregar dados retroativos do GitLab no banco.

## Arquivos

- Dockerfile de backfill: `Dockerfile.backfill`
- Workflow manual para publicar imagem: `.github/workflows/backfill-image.yml`

## Build local da imagem

```bash
docker build -f Dockerfile.backfill -t gitlab-elt-backfill:local .
```

## Execucao efemera local

Exemplo com variaveis inline:

```bash
docker run --rm \
  -e POSTGRES_HOST=localhost \
  -e POSTGRES_PORT=5432 \
  -e POSTGRES_USER=gitlab_elt \
  -e POSTGRES_PASSWORD=gitlab_elt_dev \
  -e POSTGRES_DB=gitlab_elt \
  -e GITLAB_BASE_URL=https://gitlab.com \
  -e GITLAB_TOKEN=<seu-token> \
  -e GITLAB_GROUP_IDS=<id1,id2> \
  gitlab-elt-backfill:local
```

Exemplo com arquivo `.env`:

```bash
docker run --rm --env-file .env gitlab-elt-backfill:local
```

## Variaveis obrigatorias

Obrigatorias:

- `POSTGRES_HOST`
- `POSTGRES_PORT`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_DB`
- `GITLAB_BASE_URL`
- `GITLAB_TOKEN`
- `GITLAB_PROJECT_IDS` ou `GITLAB_GROUP_IDS` (ao menos um)

Opcionais relevantes:

- `GITLAB_RATE_LIMIT` (default: `20`; no backfill o limite efetivo e capado em `10 req/s`)
- `GITLAB_RETRY_MAX` (default: `3`)

## Publicacao manual no GitHub Actions

Workflow: `.github/workflows/backfill-image.yml`

Gatilho: manual (`workflow_dispatch`).

### Secrets necessarios

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

### Variable necessaria

- `DOCKERHUB_BACKFILL_IMAGE` no formato `usuario/repositorio`

### Como rodar

1. Acesse **Actions** no GitHub.
2. Abra o workflow **Backfill Docker Image**.
3. Clique em **Run workflow**.
4. (Opcional) preencha `custom_tag`.

Tags publicadas:

- `latest`
- `sha-<commit>`
- `<custom_tag>` (ou `manual-<run_number>` se nao for informado)

## Executar imagem publicada

```bash
docker run --rm --env-file .env <usuario/repositorio>:latest
```

Como o processo e efemero, o container encerra ao finalizar o backfill.
