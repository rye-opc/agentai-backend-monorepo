# AgentAI Backend Monorepo

Go backend reference architecture for the **AI Agent Mobile App** (chat + voice) with:

- **Auth**: guest, Google sign-in, email + password
- **AI providers**: OpenAI (LLM/STT), ElevenLabs (TTS) with **BYOK** (bring your own key)
- **Backend-proxied** provider calls (consistent streaming + quotas + observability)
- **Microservice-friendly monorepo** (multi-module Go + `go.work`)

## Repo layout

All Go code lives under `backend/`:

```text
backend/
  go.work
  contracts/         # OpenAPI + proto + generated Go stubs
  libs/              # shared libs (logging/config/db/crypto) — keep small
  services/
    gateway/         # public REST (later: SSE) + auth enforcement
    auth/            # identities, sessions, BYOK credentials (encrypted)
    chat/            # conversations/messages
    media/           # signed audio uploads + audio metadata
    orchestrator/    # (scaffold) run orchestration + provider adapters
    worker/          # (scaffold) async jobs
```

## Local development (MVP scaffolding)

1) Start dependencies (Postgres/Redis/MinIO):

```bash
docker compose up -d
```

2) Run services (each in its own terminal):

```bash
cd backend
go work sync

go run ./services/auth/cmd/auth
go run ./services/chat/cmd/chat
go run ./services/media/cmd/media
go run ./services/gateway/cmd/gateway
```

3) (Optional) run migrations on startup by setting `DB_RUN_MIGRATIONS=true` for each service.

## Staging deployment (K3s via rye-charts)

This repo deploys to K3s using the `rye-opc/rye-charts` Flux + Helm GitOps setup.

- **CI**: `.github/workflows/deploy.yaml` builds/pushes per-service images (ARM64) and updates GitOps tags.
- **GitOps**: `rye-charts/apps/staging/agentai-*.yaml` (gateway/auth/chat/media/orchestrator/worker + postgres + minio).

### Required secrets

1) **GitHub Actions (this repo)**

- `DEPLOY_TOKEN`: classic PAT with `repo` + `write:packages` (push GHCR + commit to `rye-charts`)

2) **Kubernetes (staging namespace)**

The staging namespace must have a Secret named `secrets` containing:

- `agentai_postgres_admin_password`
- `agentai_postgres_user_password`
- `agentai_auth_database_url`
- `agentai_chat_database_url`
- `agentai_media_database_url`
- `agentai_orchestrator_database_url`
- `agentai_auth_jwt_private_key_pem`
- `agentai_auth_kms_master_key_base64`
- `agentai_google_oauth_client_id` (optional)
- `agentai_minio_root_user`
- `agentai_minio_root_password`

### Deploy to staging

Push to `staging` to trigger the pipeline:

```bash
git push origin staging
```

### Default URLs (staging)

- API gateway: `https://agentai.staging.ryenguyen.dev`
- S3 (MinIO): `https://agentai-s3.staging.ryenguyen.dev`

## Notes

- This repo implements **steps 1–3** of the architecture plan first (repo/contracts → schema/migrations → core auth/chat/media).
- Kubernetes, CI/CD, SSE runs, provider orchestration, and workers are scaffolded but intentionally kept minimal at this stage.

