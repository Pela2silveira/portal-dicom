# 06 Data Model

## Purpose

This artifact defines the first relational data model for the portal.

The model supports:

- remote PACS configuration
- HIS configuration
- local cache indexing
- retrieve workflows
- patient portal-owned study lists
- physician recent searches
- future session-aware viewer authorization

## Security Rule

- Raw physician passwords must never be stored in Postgres.
- Raw email verification codes must never be stored in Postgres.
- If a provider returns reusable auth material in the future, it must be stored only as encrypted provider-issued material, never as a copied password.
- Session, audit, and cache tables should prefer references, hashes, provider metadata, and expiry timestamps over secrets.
- A temporary direct MongoDB patient-identity adapter, if enabled before REST HIS integration exists, must be read-only and must not write operational state back into MongoDB.

## Core Configuration Tables

### `pacs_nodes`

Stores remote PACS node configuration and capabilities.

Key fields:

- `code`
- `name`
- `protocol`
- `priority`
- `enabled`
- `ae_title`
- `host`
- `port`
- `dicomweb_base_url`
- `supports_cmove`
- `supports_cget`
- `timeout_ms`
- `auth_type`
- `auth_config_json`

### `his_config`

Stores current HIS integration configuration.

Key fields:

- `provider`
- `enabled`
- `base_url`
- `auth_type`
- `params_json`
- `secret_refs_json`

Notes:

- The long-term target provider is REST HIS / MPI.
- A temporary backend-only `his_mongo_direct` provider may coexist as a transitional read-only source.
- Provider selection must stay behind a stable backend abstraction so the future REST provider can replace MongoDB access without changing portal-facing contracts.
- The current backend baseline already routes patient identity resolution through a provider abstraction before writing normalized values into relational tables.
- The first Mongo patient document baseline currently maps fields such as `documento`, `apellido`, `nombre`, `alias`, `sexo`, `genero`, and `fechaNacimiento` into the normalized patient identity abstraction.

## Patient Identity And Access Cache

### `patients`

Stores the local patient identity anchor for portal sessions.

Key fields:

- `document_type`
- `document_number`
- `full_name`
- `birth_date`
- `sex`
- `gender_identity` (future extension when sourced from HIS or trusted demographic systems)
- `last_his_sync_at`
- `last_login_at`

### `patient_identifiers`

Stores alternate patient identifiers resolved from HIS or remote domains.

Key fields:

- `patient_id`
- `source_system`
- `identifier_type`
- `identifier_value`
- `is_primary`
- `last_verified_at`
- `metadata_json`

Use cases:

- cache alternate HIS identifiers
- map one DNI to multiple external identifiers
- reuse known identifiers for later searches

### `patient_sessions`

Stores patient portal sessions.

Key fields:

- `patient_id`
- `status`
- `verification_channel`
- `verification_completed_at`
- `expires_at`
- `last_seen_at`
- `client_ip`
- `user_agent`

### `patient_study_access`

Stores the portal-owned patient list of known and authorized studies.

Key fields:

- `patient_id`
- `study_instance_uid`
- `authorization_basis`
- `availability_status`
- `local_orthanc_study_id`
- `first_seen_at`
- `last_seen_at`
- `last_authorized_at`
- `source_json`

Use cases:

- build the patient-facing study list
- remember known study UIDs for each patient
- decouple portal authorization logic from OHIF UI behavior

## Physician Identity, Sessions, And Recent Activity

### `physicians`

Stores the local physician identity anchor.

Key fields:

- `username`
- `dni`
- `full_name`
- `auth_provider`
- `mfa_enabled`
- `last_login_at`
- `last_success_auth_at`
- `last_failed_auth_at`

### `physician_sessions`

Stores physician portal sessions and MFA state.

Key fields:

- `physician_id`
- `status`
- `auth_provider`
- `mfa_status`
- `expires_at`
- `last_seen_at`
- `client_ip`
- `user_agent`

### `physician_recent_queries`

Stores the physician cache of recent searches.

Key fields:

- `physician_id`
- `query_json`
- `searched_at`
- `result_count`
- `expires_at`

Use cases:

- show recent searches in the physician panel
- accelerate repeated queries
- build future "continue where you left off" behavior

### `auth_events`

Stores authentication attempt metadata for patient and physician flows.

Key fields:

- `actor_type`
- `actor_id`
- `provider`
- `event_type`
- `success`
- `credential_fingerprint`
- `mfa_method`
- `source_ip`
- `user_agent`
- `error_code`
- `created_at`

Important note:

- `credential_fingerprint` is optional metadata for correlation only.
- It is not a reversible password store.

### `auth_material_cache`

Stores provider-issued reusable auth material only when future integrations require it.

Key fields:

- `actor_type`
- `actor_id`
- `provider`
- `material_type`
- `material_ciphertext`
- `expires_at`
- `last_used_at`

Important note:

- This table is for encrypted provider-issued material such as refresh tokens or delegated session artifacts.
- It is not for raw passwords.

## Search, Retrieve, And Cache Operations

### `search_requests`

Stores logical search executions.

Key fields:

- `actor_type`
- `patient_id`
- `physician_id`
- `query_json`
- `status`
- `created_at`
- `finished_at`

### `search_node_runs`

Stores per-node search execution status.

Key fields:

- `search_request_id`
- `node_id`
- `status`
- `started_at`
- `finished_at`
- `error`
- `latency_ms`
- `partial_filter`
- `unsupported_filters_json`

### `retrieve_jobs`

Stores retrieve jobs and state transitions.

Key fields:

- `study_instance_uid`
- `source_node_id`
- `requested_by_actor_type`
- `requested_by_actor_id`
- `status`
- `error`
- `created_at`
- `started_at`
- `finished_at`
- `orthanc_study_id`
- `instances_received`

### `cached_studies`

Stores the operational index of what is known in local Orthanc.

Key fields:

- `study_instance_uid`
- `orthanc_study_id`
- `first_seen_at`
- `last_verified_at`
- `expires_at`
- `cache_status`
- `locations_json`

## Audit

### `integration_audit`

Stores technical audit events with PHI-minimizing discipline.

Key fields:

- `type`
- `ref_id`
- `message`
- `data_json`
- `created_at`

## Design Notes

- Orthanc remains the source of truth for the local image cache.
- `cached_studies` is an operational index that can be rebuilt.
- Patient and physician session tables exist to support the future rule: viewer access must be validated by active session and allowed `StudyInstanceUID`.
- The data model intentionally separates:
  - identity cache
  - session state
  - provider-issued auth material
  - operational search and retrieve history
