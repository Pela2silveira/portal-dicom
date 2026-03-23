CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS pacs_nodes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code text NOT NULL UNIQUE,
  name text NOT NULL,
  protocol text NOT NULL,
  priority integer NOT NULL DEFAULT 100,
  enabled boolean NOT NULL DEFAULT true,
  ae_title text,
  host text,
  port integer,
  calling_ae_title text,
  dicomweb_base_url text,
  supports_cmove boolean NOT NULL DEFAULT false,
  supports_cget boolean NOT NULL DEFAULT false,
  timeout_ms integer NOT NULL DEFAULT 15000,
  auth_type text,
  auth_config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS his_config (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  provider text NOT NULL,
  enabled boolean NOT NULL DEFAULT false,
  base_url text NOT NULL,
  auth_type text NOT NULL,
  params_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  secret_refs_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS patients (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  document_type text NOT NULL DEFAULT 'dni',
  document_number text NOT NULL,
  full_name text,
  birth_date date,
  sex text,
  last_his_sync_at timestamptz,
  last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (document_type, document_number)
);

CREATE TABLE IF NOT EXISTS patient_identifiers (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  patient_id uuid NOT NULL REFERENCES patients(id) ON DELETE CASCADE,
  source_system text NOT NULL,
  identifier_type text NOT NULL,
  identifier_value text NOT NULL,
  is_primary boolean NOT NULL DEFAULT false,
  last_verified_at timestamptz,
  metadata_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (source_system, identifier_type, identifier_value)
);

CREATE TABLE IF NOT EXISTS patient_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  patient_id uuid NOT NULL REFERENCES patients(id) ON DELETE CASCADE,
  status text NOT NULL,
  verification_channel text,
  verification_completed_at timestamptz,
  expires_at timestamptz,
  last_seen_at timestamptz,
  client_ip inet,
  user_agent text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS patient_study_access (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  patient_id uuid NOT NULL REFERENCES patients(id) ON DELETE CASCADE,
  study_instance_uid text NOT NULL,
  authorization_basis text,
  availability_status text NOT NULL DEFAULT 'unavailable',
  local_orthanc_study_id text,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz,
  last_authorized_at timestamptz,
  source_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  UNIQUE (patient_id, study_instance_uid)
);

CREATE TABLE IF NOT EXISTS physicians (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  username text NOT NULL UNIQUE,
  dni text UNIQUE,
  full_name text,
  auth_provider text NOT NULL DEFAULT 'ldap_provincial',
  mfa_enabled boolean NOT NULL DEFAULT false,
  last_login_at timestamptz,
  last_success_auth_at timestamptz,
  last_failed_auth_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS physician_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  physician_id uuid NOT NULL REFERENCES physicians(id) ON DELETE CASCADE,
  status text NOT NULL,
  auth_provider text NOT NULL,
  mfa_status text,
  expires_at timestamptz,
  last_seen_at timestamptz,
  client_ip inet,
  user_agent text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS physician_recent_queries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  physician_id uuid NOT NULL REFERENCES physicians(id) ON DELETE CASCADE,
  query_json jsonb NOT NULL,
  result_count integer,
  searched_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz
);

CREATE TABLE IF NOT EXISTS auth_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_type text NOT NULL,
  actor_id uuid,
  provider text NOT NULL,
  event_type text NOT NULL,
  success boolean NOT NULL,
  credential_fingerprint text,
  mfa_method text,
  source_ip inet,
  user_agent text,
  error_code text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_material_cache (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_type text NOT NULL,
  actor_id uuid,
  provider text NOT NULL,
  material_type text NOT NULL,
  material_ciphertext bytea NOT NULL,
  expires_at timestamptz,
  last_used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS search_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_type text,
  patient_id uuid REFERENCES patients(id) ON DELETE SET NULL,
  physician_id uuid REFERENCES physicians(id) ON DELETE SET NULL,
  query_json jsonb NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);

CREATE TABLE IF NOT EXISTS search_node_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  search_request_id uuid NOT NULL REFERENCES search_requests(id) ON DELETE CASCADE,
  node_id uuid REFERENCES pacs_nodes(id) ON DELETE SET NULL,
  started_at timestamptz,
  finished_at timestamptz,
  status text NOT NULL,
  error text,
  latency_ms integer,
  partial_filter boolean NOT NULL DEFAULT false,
  unsupported_filters_json jsonb NOT NULL DEFAULT '[]'::jsonb
);

CREATE TABLE IF NOT EXISTS retrieve_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  study_instance_uid text NOT NULL,
  source_node_id uuid REFERENCES pacs_nodes(id) ON DELETE SET NULL,
  requested_by_actor_type text,
  requested_by_actor_id uuid,
  status text NOT NULL,
  error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  finished_at timestamptz,
  orthanc_study_id text,
  instances_received integer
);

CREATE TABLE IF NOT EXISTS cached_studies (
  study_instance_uid text PRIMARY KEY,
  orthanc_study_id text,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_verified_at timestamptz,
  expires_at timestamptz,
  cache_status text NOT NULL DEFAULT 'not_local',
  locations_json jsonb NOT NULL DEFAULT '[]'::jsonb
);

CREATE TABLE IF NOT EXISTS integration_audit (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  type text NOT NULL,
  ref_id text,
  message text NOT NULL,
  data_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_patient_identifiers_patient_id ON patient_identifiers(patient_id);
CREATE INDEX IF NOT EXISTS idx_patient_sessions_patient_id ON patient_sessions(patient_id);
CREATE INDEX IF NOT EXISTS idx_patient_study_access_patient_id ON patient_study_access(patient_id);
CREATE INDEX IF NOT EXISTS idx_patient_study_access_study_uid ON patient_study_access(study_instance_uid);
CREATE INDEX IF NOT EXISTS idx_physician_sessions_physician_id ON physician_sessions(physician_id);
CREATE INDEX IF NOT EXISTS idx_physician_recent_queries_physician_id ON physician_recent_queries(physician_id);
CREATE INDEX IF NOT EXISTS idx_auth_events_actor ON auth_events(actor_type, actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_search_requests_patient_id ON search_requests(patient_id);
CREATE INDEX IF NOT EXISTS idx_search_requests_physician_id ON search_requests(physician_id);
CREATE INDEX IF NOT EXISTS idx_search_node_runs_request_id ON search_node_runs(search_request_id);
CREATE INDEX IF NOT EXISTS idx_retrieve_jobs_study_uid ON retrieve_jobs(study_instance_uid);
CREATE INDEX IF NOT EXISTS idx_integration_audit_type_created_at ON integration_audit(type, created_at DESC);
