CREATE TABLE IF NOT EXISTS study_share_links (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  token_hash text NOT NULL UNIQUE,
  patient_id uuid REFERENCES patients(id) ON DELETE CASCADE,
  study_instance_uid text NOT NULL,
  viewer_kind text NOT NULL,
  channel text,
  status text NOT NULL DEFAULT 'active',
  max_uses integer NOT NULL DEFAULT 10,
  consumed_uses integer NOT NULL DEFAULT 0,
  expires_at timestamptz NOT NULL,
  first_opened_at timestamptz,
  last_opened_at timestamptz,
  revoked_at timestamptz,
  recipient_label text,
  recipient_contact text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_study_share_links_patient_id
  ON study_share_links(patient_id);

CREATE INDEX IF NOT EXISTS idx_study_share_links_study_uid
  ON study_share_links(study_instance_uid);
