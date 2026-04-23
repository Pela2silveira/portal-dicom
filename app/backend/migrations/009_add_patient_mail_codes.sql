CREATE TABLE IF NOT EXISTS patient_mail_codes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  patient_id uuid NOT NULL REFERENCES patients(id) ON DELETE CASCADE,
  code_hash text NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_patient_mail_codes_patient_created
  ON patient_mail_codes(patient_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_patient_mail_codes_active
  ON patient_mail_codes(patient_id, expires_at)
  WHERE consumed_at IS NULL;
