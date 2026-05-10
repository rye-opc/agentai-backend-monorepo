CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS
$$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE IF NOT EXISTS audio_objects (
  audio_ref uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  bucket text NOT NULL,
  object_key text NOT NULL UNIQUE,
  content_type text NOT NULL,
  status text NOT NULL,
  bytes bigint,
  duration_ms integer,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER audio_objects_set_updated_at
BEFORE UPDATE ON audio_objects
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE INDEX IF NOT EXISTS audio_objects_user_created_idx
ON audio_objects(user_id, created_at DESC, audio_ref DESC);

