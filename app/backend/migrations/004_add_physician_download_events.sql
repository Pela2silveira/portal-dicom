CREATE TABLE IF NOT EXISTS physician_download_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  physician_id uuid NOT NULL REFERENCES physicians(id) ON DELETE CASCADE,
  study_instance_uid text NOT NULL,
  downloaded_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_physician_download_events_physician_week
  ON physician_download_events(physician_id, downloaded_at DESC);
