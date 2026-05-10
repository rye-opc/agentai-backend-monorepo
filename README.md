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

## Notes

- This repo implements **steps 1–3** of the architecture plan first (repo/contracts → schema/migrations → core auth/chat/media).
- Kubernetes, CI/CD, SSE runs, provider orchestration, and workers are scaffolded but intentionally kept minimal at this stage.

