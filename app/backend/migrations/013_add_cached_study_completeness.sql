ALTER TABLE cached_studies
  ADD COLUMN IF NOT EXISTS expected_series_count integer,
  ADD COLUMN IF NOT EXISTS present_series_count integer,
  ADD COLUMN IF NOT EXISTS missing_series_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS last_completeness_checked_at timestamptz;
