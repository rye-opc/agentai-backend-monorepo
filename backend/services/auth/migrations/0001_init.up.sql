CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS
$$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE IF NOT EXISTS users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text UNIQUE,
  display_name text,
  is_guest boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER users_set_updated_at
BEFORE UPDATE ON users
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS identities (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider text NOT NULL,
  subject text NOT NULL,
  email text,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider, subject)
);

CREATE INDEX IF NOT EXISTS identities_user_id_idx ON identities(user_id);

CREATE TABLE IF NOT EXISTS password_credentials (
  user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  password_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER password_credentials_set_updated_at
BEFORE UPDATE ON password_credentials
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS refresh_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  refresh_token_hash bytea NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz
);

CREATE INDEX IF NOT EXISTS refresh_sessions_user_id_idx ON refresh_sessions(user_id);
CREATE INDEX IF NOT EXISTS refresh_sessions_expires_at_idx ON refresh_sessions(expires_at);

CREATE TABLE IF NOT EXISTS entitlements (
  user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  plan text NOT NULL DEFAULT 'free',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER entitlements_set_updated_at
BEFORE UPDATE ON entitlements
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS provider_credentials (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider text NOT NULL,
  label text NOT NULL DEFAULT '',
  secret_ciphertext bytea NOT NULL,
  dek_ciphertext bytea NOT NULL,
  kms_key_id text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  UNIQUE (user_id, provider, label)
);

CREATE INDEX IF NOT EXISTS provider_credentials_user_provider_idx ON provider_credentials(user_id, provider);

CREATE TRIGGER provider_credentials_set_updated_at
BEFORE UPDATE ON provider_credentials
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();

