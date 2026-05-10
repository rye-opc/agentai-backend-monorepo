-- Create per-service databases (same cluster) for local dev.
DO
$$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_database WHERE datname = 'agentai_auth') THEN
    CREATE DATABASE agentai_auth OWNER agentai;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_database WHERE datname = 'agentai_chat') THEN
    CREATE DATABASE agentai_chat OWNER agentai;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_database WHERE datname = 'agentai_media') THEN
    CREATE DATABASE agentai_media OWNER agentai;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_database WHERE datname = 'agentai_orchestrator') THEN
    CREATE DATABASE agentai_orchestrator OWNER agentai;
  END IF;
END
$$;

