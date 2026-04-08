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

## Open UI follow-ups

- Review the patient login screen on mobile: the access block still looks visually narrower than the professional login on some devices and needs a focused responsive pass.
- Review and optimize the full professional search section for mobile, including filters, actions, and result readability.
- Review refresh behavior around retrieve flows, especially on mobile, to ensure the UI does not jump, lose context, or reflow awkwardly while retrieve status updates arrive.
  - Debug first:
    - verify whether `watchPatientRetrieveJob()` / `watchPhysicianRetrieveJob()` receive the terminal `done` state
    - verify whether `loadPatientStudies(..., { silentRefresh: true })` and `loadPhysicianResults(..., { silentRefresh: true })` are triggered after retrieve completion
    - verify whether backend state is already `done` / `available_local` while the frontend remains stale until manual refresh
- Review non-visual accessibility end-to-end so the app becomes usable for blind users:
  - keyboard-only navigation and focus order across login, workspaces, filters, results, viewer access, and retrieve flows
  - screen-reader semantics for custom controls, especially role selector, date pickers, tooltips, and dynamic result lists
  - field-level error announcement and live-region messaging for login, retrieve progress, and session expiration
  - avoid relying only on color for error/status communication
  - run a dedicated pass with real assistive technology targets (VoiceOver / TalkBack / NVDA as applicable)
- Debate a preview screen for low-image-count studies, available to both patient and professional:
  - evaluate whether Orthanc preview/rendered-image APIs are sufficient to show thumbnails and per-image JPG exports for local studies with very few renderable instances
  - constrain the discussion to convenience preview/export only, not diagnostic replacement for the viewer or DICOM download
  - capture risks already identified:
    - a JPG can be mistaken for a complete or diagnostically sufficient study artifact
    - not every study with 1-5 images is clinically simple or safe to summarize visually
    - some modalities/encodings may not render consistently through Orthanc core APIs without plugin support
    - multiframe / cine / secondary-capture edge cases complicate the notion of “download the study as JPG”
- Add professional login hardening against repeated credential failures:
  - define temporary user lockout behavior after consecutive failed password attempts
  - decide storage, expiration window, reset policy after successful login, and audit logging without leaking sensitive details
- Define and implement a weekly login limit policy where applicable:
  - clarify whether the limit applies to patient logins, professional logins, or both
  - define how the weekly window is measured, how the user is informed, and what operational exceptions or resets are needed
