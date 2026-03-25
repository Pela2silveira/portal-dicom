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
- `protocol`: currently expected as `qido_rs`
- `priority`: numeric priority, lower means more preferred
- `aet`: remote AE title
- `dicom_host`: DIMSE hostname or IP
- `dicom_port`: DIMSE port
- `dicomweb_base_url`: remote DICOMweb base URL
- `supports_cmove`: whether the node supports C-MOVE
- `supports_cget`: whether the node supports C-GET
- `auth`: authentication config for DICOMweb access

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
  "protocol": "qido_rs",
  "priority": 1,
  "aet": "PACSHPN",
  "dicom_host": "172.16.1.205",
  "dicom_port": 11112,
  "dicomweb_base_url": "https://pacshpn.andes.gob.ar/dcm4chee-arc/aets/PACSHPN/rs",
  "supports_cmove": true,
  "supports_cget": true,
  "auth": {
    "type": "keycloak_client_credentials",
    "token_url": "https://keycloak-pacshpn.andes.gob.ar/auth/realms/dcm4che/protocol/openid-connect/token",
    "client_id_env": "PACS_HPN_CLIENT_ID",
    "client_secret_env": "PACS_HPN_CLIENT_SECRET"
  }
}
```

## `his`

Controls the patient/professional identity provider integration.

Fields:

- `provider`: integration mode, for example `his_mongo_direct` or `andes`
- `enabled`: informational flag for the configured integration
- `base_url`: upstream HIS base URL when applicable
- `auth_type`: auth mode label for the upstream system
- `document_lookup_path`: patient lookup template path

Notes:

- When `provider = "his_mongo_direct"`, the backend expects Mongo env vars such as `HIS_MONGO_URI` and `HIS_MONGO_DATABASE`.
- Professional transitional validation against Mongo `profesional` also depends on this provider mode.

## `patient`

Current patient access behavior flags.

Fields:

- `fake_auth`: when `true`, patient access keeps the current demo behavior and skips real mail-code delivery

## `professional`

Current professional access behavior flags.

Fields:

- `fake_auth`: when `true`, professional access uses the current transitional validation flow instead of real LDAP/MFA
- `initial_cache_period`: controls the no-filter result window loaded after professional login from Orthanc local cache

Accepted `initial_cache_period` values:

- `today`
- `current_week`
- `week`
- `current_month`
- `month`
- `current_year`
- `year`

Recommended default:

```json
{
  "fake_auth": true,
  "initial_cache_period": "current_week"
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
