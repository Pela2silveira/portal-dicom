CREATE TABLE IF NOT EXISTS qido_study_cache (
  study_instance_uid text NOT NULL,
  source_node_id text NOT NULL,
  study_date text,
  patient_name text,
  patient_id text,
  study_description text,
  modalities_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  locations_json jsonb NOT NULL DEFAULT '[]'::jsonb,
  andes_prestacion_id text,
  andes_prestacion text,
  andes_professional text,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  last_andes_enriched_at timestamptz,
  PRIMARY KEY (study_instance_uid, source_node_id)
);

CREATE INDEX IF NOT EXISTS idx_qido_study_cache_study_uid ON qido_study_cache(study_instance_uid);
CREATE INDEX IF NOT EXISTS idx_qido_study_cache_source_node_id ON qido_study_cache(source_node_id);
CREATE INDEX IF NOT EXISTS idx_qido_study_cache_last_seen_at ON qido_study_cache(last_seen_at DESC);
