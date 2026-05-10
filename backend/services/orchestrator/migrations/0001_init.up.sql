CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS
$$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE IF NOT EXISTS runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  conversation_id uuid,
  status text NOT NULL,
  config jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);

CREATE TRIGGER runs_set_updated_at
BEFORE UPDATE ON runs
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS runs_user_created_idx ON runs(user_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS run_attempts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  stage text NOT NULL, -- stt|llm|tts
  provider text,
  model text,
  started_at timestamptz,
  ended_at timestamptz,
  error_message text,
  cost_usd numeric(12,6),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS run_attempts_run_id_idx ON run_attempts(run_id);

CREATE TABLE IF NOT EXISTS usage_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  run_id uuid,
  provider text,
  kind text,
  units bigint,
  cost_usd numeric(12,6),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS usage_events_user_created_idx ON usage_events(user_id, created_at DESC, id DESC);

