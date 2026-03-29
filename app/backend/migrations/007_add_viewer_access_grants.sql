ALTER TABLE patient_sessions
  ADD COLUMN IF NOT EXISTS token_hash text;

ALTER TABLE physician_sessions
  ADD COLUMN IF NOT EXISTS token_hash text;

CREATE UNIQUE INDEX IF NOT EXISTS idx_patient_sessions_token_hash
  ON patient_sessions(token_hash)
  WHERE token_hash IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_physician_sessions_token_hash
  ON physician_sessions(token_hash)
  WHERE token_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS viewer_access_grants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  token_hash text NOT NULL UNIQUE,
  subject_type text NOT NULL,
  patient_session_id uuid REFERENCES patient_sessions(id) ON DELETE CASCADE,
  physician_session_id uuid REFERENCES physician_sessions(id) ON DELETE CASCADE,
  study_instance_uid text NOT NULL,
  viewer_kind text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  max_uses integer NOT NULL DEFAULT 1,
  consumed_uses integer NOT NULL DEFAULT 0,
  expires_at timestamptz NOT NULL,
  first_opened_at timestamptz,
  last_opened_at timestamptz,
  revoked_at timestamptz,
  client_ip inet,
  user_agent text,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT viewer_access_grants_single_subject_chk CHECK (
    ((patient_session_id IS NOT NULL)::int + (physician_session_id IS NOT NULL)::int) <= 1
  )
);

CREATE INDEX IF NOT EXISTS idx_viewer_access_grants_patient_session_id
  ON viewer_access_grants(patient_session_id);

CREATE INDEX IF NOT EXISTS idx_viewer_access_grants_physician_session_id
  ON viewer_access_grants(physician_session_id);

CREATE INDEX IF NOT EXISTS idx_viewer_access_grants_study_uid
  ON viewer_access_grants(study_instance_uid);
