# Config JSON Guide

`app/config/config.json` is the local runtime configuration file for the portal stack.

This file is local-only and must not be committed.
Use [`config.example.json`](./config.example.json) as the starting point.

## Recommended workflow

1. Copy the example file.
2. Replace placeholder values with your environment values.
3. Keep secrets out of the JSON file itself; reference them through env vars when the field supports it.

Example:

```bash
cp app/config/config.example.json app/config/config.json
```

## Top-level structure

The file currently supports:

```json
{
  "pacs_nodes": [],
  "his": {},
  "portal": {},
  "patient": {},
  "professional": {},
  "cache": {}
}
```

## `pacs_nodes`

List of remote PACS nodes available to the backend.

Required fields per node:

- `id`: stable internal code for the node
- `name`: visible name
- `andes_organization_id`: optional ANDES organization id used to enrich professional QIDO results from Mongo `prestaciones`
- `protocol`: high-level node kind, for example `dicomweb`, `dimse`, or `hybrid`
- `priority`: numeric priority, lower means more preferred
- `search`: search capability config
- `retrieve`: retrieve capability config
- `health`: health-check mode

`search` fields:

- `mode`: `qido_rs` or `c_find`
- `dicomweb_base_url`: required for `qido_rs`
- `auth`: authentication config for DICOMweb access when needed

`retrieve` fields:

- `mode`: `c_move`, `c_get`, or empty when retrieve is not yet defined
- `aet`: remote AE title for DIMSE retrieve
- `dicom_host`: DIMSE hostname or IP
- `dicom_port`: DIMSE port
- `supports_cmove`: whether the node supports C-MOVE
- `supports_cget`: whether the node supports C-GET

`health` fields:

- `mode`: `http`, `auth_qido`, or `dimse_c_echo`

`auth` fields:

- `type`: currently `keycloak_client_credentials` for the current integration
- `token_url`: OAuth token endpoint
- `client_id_env`: env var name containing the client id
- `client_secret_env`: env var name containing the client secret

Example:

```json
{
  "id": "hpn",
  "name": "DCM4CHEE HPN",
  "andes_organization_id": "57e9670e52df311059bc8964",
  "protocol": "hybrid",
  "priority": 1,
  "search": {
    "mode": "qido_rs",
    "dicomweb_base_url": "https://pacshpn.andes.gob.ar/dcm4chee-arc/aets/PACSHPN/rs",
    "auth": {
      "type": "keycloak_client_credentials",
      "token_url": "https://keycloak-pacshpn.andes.gob.ar/auth/realms/dcm4che/protocol/openid-connect/token",
      "client_id_env": "PACS_HPN_CLIENT_ID",
      "client_secret_env": "PACS_HPN_CLIENT_SECRET"
    }
  },
  "retrieve": {
    "mode": "c_get",
    "aet": "PACSHPN",
    "dicom_host": "172.16.1.205",
    "dicom_port": 11112,
    "supports_cmove": true,
    "supports_cget": true
  },
  "health": {
    "mode": "dimse_c_echo"
  }
}
```

Compatibility note:

- The backend still accepts the previous flat fields such as `dicomweb_base_url`, `aet`, `dicom_host`, `dicom_port`, `supports_cmove`, `supports_cget`, and top-level `auth`.
- New config should prefer `search` / `retrieve` / `health`.

## `his`

Controls the patient/professional identity provider integration.

Fields:

- `provider`: integration mode, for example `his_mongo_direct` or `andes`
- `enabled`: informational flag for the configured integration
- `prestaciones_enrichment_enabled`: optional feature flag, disabled by default; when `true` and `provider = his_mongo_direct`, the backend connects to Mongo `prestaciones` to enrich study cards with `Prestación en ANDES` / `Profesional en ANDES`
- `base_url`: upstream HIS base URL when applicable
- `auth_type`: auth mode label for the upstream system
- `document_lookup_path`: patient lookup template path

Notes:

- When `provider = "his_mongo_direct"`, the backend expects Mongo env vars such as `HIS_MONGO_URI` and `HIS_MONGO_DATABASE`.
- Professional transitional validation against Mongo `profesional` also depends on this provider mode.

## `patient`

Current patient access behavior flags.

Fields:

- `auth_mode`: patient auth mode. Accepted values:
  - `mail`: final production flow, requires an active patient email and preserves the `Documento + código por mail` UX; current code already prevalidates contact and creates a real backend session, but the final mail delivery / one-time-code verification is still pending
  - `fake_auth`: demo flow, validates patient existence but skips real mail-code delivery
  - `master_key`: transitional operational bypass, validates patient existence and uses one shared configured key for patient access while the real `mail` delivery/verification integration is incomplete
- `master_key`: required when `auth_mode` is `master_key`

Operational note:

- `mail` is the intended steady-state patient authentication mode for production.
- `master_key` is not the target product design; keep it only as a temporary fallback for controlled environments.

Backward compatibility:

- `fake_auth` is still accepted as a legacy fallback. If `auth_mode` is omitted and `fake_auth = true`, the backend resolves the mode as `fake_auth`.

## `portal`

Shared portal runtime behavior.

Fields:

- `session_timeout_minutes`: common session timeout for patient and professional surfaces
- `show_demo_ribbon`: when `true`, the diagonal `Demo` ribbon is shown on the landing auth card and on both patient/professional workspaces
- `retrieve_progress_poll_seconds`: polling interval, in seconds, used by the backend to query Orthanc job progress for active retrieve jobs
- `retrieve_worker_concurrency`: number of backend goroutines allowed to monitor/complete Orthanc-backed retrieve jobs concurrently
- `scheduled_retrieve_enabled`: when `true`, a background scheduler periodically enqueues automatic retrieves for recent non-local studies already present in the portal lists/cache
- `scheduled_retrieve_interval_minutes`: interval between scheduler cycles
- `scheduled_retrieve_max_study_age_days`: maximum study age considered by the scheduler, based on study date or last seen date fallback
- `scheduled_retrieve_batch_size`: maximum number of studies enqueued per scheduler cycle

Public exposure note:

- The full `/api/config` endpoint is operational/internal and stays blocked by public Nginx.
- The landing UI reads only a minimal public runtime payload from `/api/runtime-config`, currently limited to safe `portal` fields such as `session_timeout_minutes` and `show_demo_ribbon`, plus the effective patient `auth_mode` needed to adapt the code-entry input.

## `professional`

Current professional access behavior flags.

Fields:

- `fake_auth`: when `true`, professional access uses the current transitional validation flow instead of real LDAP/MFA
- `initial_cache_period`: controls the no-filter result window loaded after professional login from Orthanc local cache
- `weekly_download_limit`: maximum number of full-study DICOM ZIP downloads a professional can trigger during a calendar week
- `license_exceptions`: optional list of DNI/username entries that may access the portal bypassing both active matrícula and `habilitado == true`

When `fake_auth` is `false`, the current implementation expects:

- `LDAP_HOST`
- `LDAP_PORT`
- `LDAP_OU`

Current LDAP behavior:

- insecure `ldap://` connection
- direct bind using `uid=<dni>,<LDAP_OU>`
- Mongo `profesional` remains the source for displayed identity and for standard authorization rules outside the configured exception list

Accepted `initial_cache_period` values:

- `today`
- `current_week`
- `week`
- `current_month`
- `month`
- `current_year`
- `year`

Recommended default:

Patient example:

```json
{
  "auth_mode": "mail",
  "master_key": ""
}
```

Professional example:

```json
{
  "fake_auth": true,
  "initial_cache_period": "current_week",
  "weekly_download_limit": 100,
  "license_exceptions": []
}
```

## `cache`

Local cache behavior.

Fields:

- `orthanc_base_url`: internal Orthanc base URL
- `retention_days`: retention target for local cached studies

## Notes

- `app/config/config.json` is ignored and should remain local to the environment.
- Only update [`config.example.json`](./config.example.json) when the shared config shape changes.
- Keep tokens, passwords, and private host-specific secrets out of tracked files.
