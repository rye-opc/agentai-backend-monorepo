-- Create per-service databases (same cluster) for local dev.
--
-- Note: `CREATE DATABASE` cannot run inside a DO block / function.
-- We use psql's `\gexec` to conditionally execute CREATE statements.

SELECT format('CREATE DATABASE %I OWNER %I', 'agentai_auth', 'agentai')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'agentai_auth')
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', 'agentai_chat', 'agentai')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'agentai_chat')
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', 'agentai_media', 'agentai')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'agentai_media')
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', 'agentai_orchestrator', 'agentai')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'agentai_orchestrator')
\gexec

