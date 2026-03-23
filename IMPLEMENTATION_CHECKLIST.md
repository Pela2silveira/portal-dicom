# Implementation Checklist

This checklist turns the approved MVP plan into a strict execution order for the repository.

## Phase 1: stack skeleton

- Create `docker-compose.yml` with:
  - `nginx`
  - `backend`
  - `postgres`
  - `orthanc`
  - `ohif`
- Create Docker networks:
  - `frontend_net`
  - `backend_net`
- Create persistent volumes:
  - `postgres-data`
  - `orthanc-db`
  - `orthanc-storage`
- Add `backend` Dockerfile.
- Add `nginx` config with these routes:
  - `/`
  - `/api/`
  - `/ohif/`
  - `/dicomweb/`
- Deny Orthanc admin REST paths through Nginx.
- Configure OHIF to consume only `/dicomweb/`.
- Configure Orthanc local DICOM port exposure for remote PACS connectivity.

## Phase 2: backend foundation

- Create Go module for backend service.
- Add configuration loader for:
  - app config file path
  - Postgres connection
  - Orthanc connection
  - PACS node secrets from env vars
  - HIS secrets from env vars
- Add structured JSON logging.
- Add `GET /api/health`.
- Add health checks for:
  - Postgres
  - Orthanc HTTP
- Return service status in health response.

## Phase 3: persistence and config

- Add migration system.
- Create initial tables:
  - `pacs_nodes`
  - `his_config`
  - `retrieve_jobs`
  - `integration_audit`
- Add optional early tables if needed immediately:
  - `search_requests`
  - `search_node_runs`
  - `cached_studies`
- Add startup migration execution.
- Add config bootstrap from mounted `config.json`.
- Implement `GET /api/config` as read-only.
- Ensure `client_secret`, tokens, and secret refs are never returned by the API.

## Phase 4: minimal operator UI

- Add static index page served by Nginx.
- Show:
  - backend health
  - Orthanc health
  - configured remote PACS nodes
  - basic system status
- Do not add full search UI yet unless Phase 5 is started.

## Phase 5: retrieve first slice

- Add `POST /api/retrieve`.
- Add `GET /api/retrieve/{job_id}`.
- Persist `retrieve_jobs` state transitions:
  - `queued`
  - `running`
  - `done`
  - `failed`
  - `failed_timeout`
- Implement `C-MOVE` as the first retrieve path.
- Target local Orthanc as destination AE.
- Poll Orthanc for retrieve completion.
- Implement stable window and global timeout.
- Add Retrieve action in UI.
- Enable Visualizar only after `done`.
- Verify OHIF opens the retrieved study through `/dicomweb/`.

## Phase 6: search workflow

- Add `POST /api/search`.
- Add `GET /api/search/{id}/events` with SSE.
- Implement `QIDORSHandler`.
- Implement Keycloak token acquisition with `client_credentials`.
- Cache PACS access tokens with TTL.
- Add dedup by `StudyInstanceUID`.
- Prefer metadata from highest-priority node.
- Add minimal search UI with fields:
  - `patient_id`
  - `patient_name`
  - `date_from`
  - `date_to`
  - `modalities`
- Render incremental SSE results.
- Show required columns:
  - `PatientName`
  - `PatientID`
  - `StudyDate`
  - `StudyTime`
  - `ModalitiesInStudy`
  - `StudyDescription`
  - `source nodes`
  - `cache status`

## Phase 7: C-GET support

- Confirm exact Orthanc REST endpoint and payload for remote retrieve via `C-GET`.
- Implement per-node retrieve method:
  - `move`
  - `get`
  - `auto`
- Trigger `C-GET` through Orthanc REST.
- Reuse retrieve polling and job state logic.

## Phase 8: retention and cleanup

- Add backend cleanup job for Orthanc cache retention:
  - default 7 days
- Add backend cleanup job for Postgres operational tables:
  - default 30 days
- Skip deletion for in-progress retrieve jobs.
- Audit cleanup runs without leaking PHI.

## Phase 9: hardening

- Ensure logs minimize PHI.
- Ensure tokens and secrets are never logged.
- Add Nginx proxy restrictions and timeouts.
- Add basic tests for:
  - health
  - retrieve job lifecycle
  - config loading
  - dedup logic
- Add runbook for local setup and troubleshooting.

## First coding target

The first coding target should be only this:

1. Compose stack.
2. Nginx routing.
3. Backend skeleton with `/api/health`.
4. Postgres migrations for config and retrieve jobs.
5. Static status page.
6. OHIF loading through Nginx against local Orthanc.

Do not start with full search or C-GET before this is working.
