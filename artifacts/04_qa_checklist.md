# MVP QA Gap Checklist — Portal DICOM Agregador + Caché (Orthanc) + OHIF

## Security
- [Ready] **Single public HTTP entrypoint**: only Nginx exposed on `http://localhost:8081`; backend/Orthanc HTTP not directly exposed.
- [Ready] **Orthanc admin REST not exposed**: Nginx denies non‑DICOMweb Orthanc routes (explicit 403/404).
- [Ready] **DICOMweb-only viewer data path**: OHIF configured to consume only `/dicom-web/` (Orthanc local) and never remote PACS.
- [Ready] **Secrets not committed**: config uses `*_secret_ref` / env/file refs; runtime `config.json` is local-only and ignored.
- [Ready] **Server-side portal sessions**: patient and professional login create backend sessions with expiry and explicit logout invalidation.
- [Missing] **Hardening for non-localhost environments**: minimal protections when deployed beyond localhost (TLS termination, IP allowlist, shared operator key, etc.).
- [Missing] **Token handling hardening**: Keycloak token cache/refresh policy (TTL, refresh-on-401) + explicit guarantee tokens/secrets never appear in logs.
- [Missing] **PHI minimization policy enforcement**: explicit list of fields allowed in `integration_audit`, `search_*` tables, and logs (and automated checks).
- [Needs Decision] **Network exposure of DIMSE ports**: confirm when/where Orthanc DICOM `4242` is published and reachable from remote PACS (VPN/NAT/firewall model).

## Functional
- [Ready] **Landing + portal surfaces**: patient/professional mock flows route to portal-owned pages first; responsive requirement captured.
- [Ready] **Patient list contract**: `GET /api/patient/studies` is resolved from the active patient session, returns `200` with `studies: []` if none, and does not trust caller-supplied document identifiers for authorization.
- [Ready] **Patient async search worker**: remote patient search runs through `search_requests`/`search_node_runs`, started by `POST /api/patient/search`, polled through `GET /api/patient/search?request_id=...`, with visible `queued/running` state in the patient results surface.
- [Ready] **Patient in-flight search reuse**: if the patient UI repeats the same `document + filters` while a search is already `queued` or `running`, the browser reuses the active `request_id` instead of emitting another redundant `POST /api/patient/search`.
- [Ready] **Stale patient search reconciliation**: patient searches left in `queued` or `running` after backend restart are reconciled to `failed`, so the portal does not restore a phantom in-progress state from persisted rows.
- [Ready] **Manual retrieve contract**: `POST /api/patient/retrieve`, `POST /api/physician/retrieve`, job persistence and state transitions exist (queued→running→done/failed).
- [Ready] **Protected non-viewer routes bound to session**: patient studies/search/retrieve/download and physician results/retrieve/download authorize from the active backend session instead of trusting request `document_number` / `username`.
- [Ready] **Transactional local availability**: a patient study changes to `available_local` only when Orthanc confirms local presence, and that promotion is committed together with the successful retrieve completion and local cache update.
- [Ready] **Viewer handoff**: portal opens `GET /ohif/viewer?StudyInstanceUIDs=<uid>` in a new tab; Visualizar enabled only when local cache is ready.
- [Ready] **OHIF root containment**: `GET /ohif/` redirects to landing and patient/professional flows enter OHIF only through study-specific viewer URLs.
- [Ready] **Retrieve completion heuristic**: Orthanc polling with stable window + global timeout defined.
- [Ready] **Hospital/source labeling in results**: patient and professional result cards show the configured PACS node display name (`pacs_nodes.name`) as the hospital/sede label instead of exposing only technical node identifiers.
- [Ready] **ANDES enrichment feature flag**: `his.prestaciones_enrichment_enabled` defaults to `false`; with the flag off, patient/professional searches do not connect to Mongo `prestaciones`, and with the flag on the result cards expose `Prestación en ANDES` and `Profesional en ANDES` when a match exists.
- [Ready] **ANDES prestación id persistence**: when Mongo `prestaciones` returns a match, the portal persists the prestación `_id` as `andes_prestacion_id` in the stored study/result payloads, even if the UI does not render that identifier.
- [Ready] **Shared QIDO cache**: remote QIDO results are persisted in PostgreSQL by `study_instance_uid + source_node_id`, so patient and professional flows can reuse the same canonical study metadata cache.
- [Ready] **Shared ANDES reuse**: when ANDES enrichment has already been resolved for a cached `study_instance_uid + source_node_id`, subsequent patient/professional searches reuse the persisted values instead of requiring a fresh Mongo lookup.
- [Open] **ANDES PDF recovery by API**: investigate whether prestaciones-related PDFs should be retrievable through an ANDES API as part of the enrichment layer, including endpoint/auth details and actor-visible UX rules.
- [Open] **Cache invalidation policy**: the product still needs a defined mechanism to purge cached studies that disappear from a PACS and to refresh stale ANDES enrichment.
- [Open] **Multiorigin result contract**: before enabling professional multiselect PACS, the API contract still needs to evolve from single `source_node_id` to aggregated `source_node_ids[]` by `StudyInstanceUID`.
- [Ready] **Professional PACS health legend**: the physician results card shows `PACS en línea X/Y` from remote PACS health checks and exposes online/offline node names on hover/focus.
- [Ready] **Professional source selector and bounded remote search**: the physician panel exposes `Cache local` plus currently online remote PACS as explicit origins; local searches run against Orthanc cache, remote searches hit only the selected PACS, and empty remote searches are rejected before broad fan-out.
- [Ready] **Professional remote retrieve source resolution**: physician remote-search results persist `source_node_id`, and `POST /api/physician/retrieve` resolves the retrieve node from the most recent persisted query result instead of guessing from local cache heuristics.
- [Ready] **Deferred retrieve list refresh**: patient and professional grids no longer refetch the full list immediately after queueing a retrieve job; they refresh only when the retrieve SSE reports terminal status (`done` or `failed`) or the stream errors.
- [Ready] **Silent patient terminal refresh**: when a patient retrieve reaches `done` or `failed`, the study list refreshes in place without replacing the grid with a loading placeholder, and preserves viewport/focus around the retrieved study.
- [Ready] **Silent physician terminal refresh**: when a professional retrieve reaches `done` or `failed`, the results grid refreshes in place without replacing the list with a loading placeholder, and preserves viewport/focus around the retrieved study.
- [Ready] **Retrieve blocked for offline source PACS**: patient and professional results expose origin-node availability, render `Origen no disponible` as a disabled action when the source PACS is offline, and the backend refuses retrieve enqueue attempts in that state.
- [Ready] **Landing role-selector focus flow**: the public landing starts with focus on the active role button; natural `Tab` moves between `Paciente` and `Profesional`; and clicking or pressing `Enter` on either role jumps to that role's user-identification input.
- [Ready] **Patient entry keyboard flow**: once the patient input flow starts, `Tab` from `Documento` moves to `Enviar código`; a successful send-code action moves focus to `Código por mail`; and `Tab`/`Enter` from the code input moves focus to `Continuar`.
- [Ready] **Professional entry keyboard flow**: once the professional input flow starts, `Tab`/`Enter` from `DNI` moves to `Contraseña`; and `Tab`/`Enter` from `Contraseña` moves focus to `Continuar`.
- [Ready] **Matching public continue CTA**: the `Continuar` button in the professional login uses the same visual treatment as the patient `Continuar` action.
- [Ready] **Demo ribbon on landing**: the public auth card shows a diagonal `Demo` ribbon on the right without interfering with focus, clicks, or responsive layout.
- [Ready] **Shared demo ribbon flag**: `portal.show_demo_ribbon` controls the same diagonal `Demo` ribbon across landing, patient workspace, and professional workspace.
- [Ready] **Patient continue backend validation**: the patient `Continuar` action validates against backend before opening the workspace; in `master_key` mode the entered code must match the configured shared key.
- [Ready] **Masked patient code input in master key mode**: when `patient.auth_mode = master_key`, the patient code field renders as hidden/password-style input instead of visible plain text.
- [Ready] **Login rate limit by IP and identifier**: patient send-code, patient login, and physician login enforce lightweight backend rate limits by effective client IP and normalized identifier, returning `429` plus `Retry-After` when exceeded.
- [Ready] **Orthanc modality reuse for DIMSE health/retrieve**: backend reuses registered Orthanc modalities in memory instead of reissuing the same `PUT /modalities/{id}` on every health check, and refreshes them if Orthanc reports the modality as missing.
- [Ready] **Orthanc internal-vs-viewer auth split**: browser/viewer access still requires a valid viewer grant cookie, while backend internal calls to Orthanc use `X-Orthanc-Internal-Token` and the authorization plugin `user profile` + `Permissions` path for admin/internal routes (`/modalities/*`, `/jobs/*`, local archives); viewer cookies only receive a narrow `tools/find` permission required by the plugin internals during study viewing.
- [Ready] **Orthanc local-study lookup under auth plugin**: backend internal local-presence checks use `POST /tools/find` instead of internal QIDO `GET /dicom-web/studies`, avoiding `403 Unknown resource` failures during patient/professional remote-search cache reconciliation.
- [Ready] **Leve aumento de timeout para health DIMSE**: los checks `C-ECHO` usan un timeout levemente mayor que el original, sin agregar gracia de arranque ni warm-up en background.
- [Ready] **Configurable shared session timeout**: patient and professional workspaces expire according to `portal.session_timeout_minutes`; when the timeout is reached, the app returns to the landing and clears the restorable workspace state.
- [Ready] **Minimal public runtime config**: the landing/workspace shell reads `portal.session_timeout_minutes` from `/api/runtime-config` without exposing the broader `/api/config` payload.
- [Ready] **Retrieve progress in row/action state**: active patient and professional retrieves expose a lightweight progress signal in the result row and retrieve button, based on retrieve phase plus Orthanc job percentage when available.
- [Ready] **Configurable retrieve progress polling**: backend polling of Orthanc job progress for active retrieves is configurable through `portal.retrieve_progress_poll_seconds` instead of being hardcoded.
- [Ready] **Configurable scheduled precache retrieve**: backend can optionally enqueue automatic Orthanc-backed retrieves for recent non-local studies already present in `qido_study_cache`, with config knobs for enablement, interval, max study age, batch size, and retrieve worker concurrency.
- [Open] **Alternative retrieve-progress strategy**: if Orthanc job percentage keeps being too sparse for UX, evaluate a more granular path such as a dedicated handler using `dcmtk` or similar tooling, and compare observability vs operational complexity.
- [Open] **Shared-volume local indexing experiment**: evaluate whether combining backend orchestration with Orthanc local indexing/plugin signals over a shared volume can improve progress visibility or local-ingest detection without destabilizing the stack.
- [Ready] **Landing form cleanup on exit/reset**: when the user presses `Salir`, or when the portal returns to the public landing without a restorable workspace, patient and professional login fields are cleared and do not retain the prior DNI, code, or password values.
- [Ready] **Soft landing reset on health degradation**: if the open portal UI receives system-health `unavailable` or confirms `/api/health = 503` after an SSE error, it returns to the landing without a full browser navigation or visible hard reload.
- [Ready] **Professional access exceptions by config**: `professional.license_exceptions` can authorize a bounded list of DNI/username entries bypassing both active matrícula and `habilitado == true`.
- [Ready] **First LDAP professional login slice**: when `professional.fake_auth = false`, `POST /api/physician/login` authenticates against LDAP using `LDAP_HOST`, `LDAP_PORT`, `LDAP_OU` and direct `uid=<dni>,<LDAP_OU>` bind before applying Mongo-based authorization.
- [Ready] **AccessionNumber diagnostic probe**: remote and local QIDO parsing now requests `AccessionNumber (00080050)`, attempts Base64 decode when plausible, and logs raw/decoded values to confirm the upstream encoding contract.
- [Missing] **Federated search**: `POST /api/search` + SSE events + dedup by `StudyInstanceUID` across ≥2 nodes not implemented (Milestone 4 pending).
- [Missing] **Professional async panel backed by real multi-node search**: current “recent queries fallback” is acceptable but not the target behavior.
- [Needs Decision] **Patient identifier strategy**: reliance on `PatientID == DNI` vs configurable mapping / HIS resolution (to avoid hard coupling).

## Integration
- [Ready] **Orthanc ↔ backend orchestration**: backend coordinates retrieve via Orthanc REST (`PUT /modalities/{id}`, `POST /modalities/{id}/get`) and polls Orthanc for presence.
- [Ready] **Retrieve proxy timeout budget**: Nginx `/api/` upstream timeout must allow long-running patient/professional retrieves instead of aborting them around 60s.
- [Ready] **PACS REST auth approach**: Keycloak `client_credentials` specified; backend uses `Authorization: Bearer <token>`.
- [Ready] **Externalized PACS node config**: per-node protocol (QIDO-RS / C-FIND), timeouts, and retrieve preference (C-MOVE/C-GET) are modeled.
- [Missing] **Second PACS node for repeatable validation**: required to test federated search/dedup deterministically (recommend simulated remote Orthanc in compose/CI).
- [Missing] **Legacy DIMSE (C-FIND) implementation choice**: confirm dcmtk-in-container vs Go library + licensing/packaging approach.
- [Needs Decision] **C-MOVE vs C-GET per node**: confirmed capability matrix for each remote node (C-MOVE allowed? destination AE routing works?).
- [Needs Decision] **HIS/ANDES config completeness**: base URL, auth method, API key format, required params, and whether MPI lookup is invoked in MVP or config-only (spec says config-first; patient-ID mapping still impacts behavior).

## Operability
- [Ready] **One-command startup**: `docker compose up` brings up `nginx`, `backend`, `postgres`, `orthanc`, `ohif`.
- [Ready] **DB migrations at startup**: schema bootstraps automatically; config loader upserts nodes + HIS config.
- [Ready] **Health endpoint**: `/api/health` reports `db_ok` and `orthanc_ok`.
- [Ready] **Container liveness**: Docker Compose checks `/api/livez` so backend can stay `healthy` while `/api/health` is degraded and Nginx serves maintenance.
- [Ready] **Maintenance fallback**: when backend health is degraded or the upstream is unavailable, Nginx serves a static maintenance page for `/` instead of surfacing a raw upstream error.
- [Ready] **Continuous Mongo degradation**: if Mongo becomes unavailable after backend startup, `/api/health` degrades again and the landing falls back to maintenance until connectivity returns.
- [Ready] **Optional remote PACS degradation**: remote PACS health is reported as optional and must not trigger maintenance mode for the whole app by itself.
- [Ready] **Remote PACS health modes**: optional PACS health supports explicit modes such as authenticated QIDO and DIMSE C-ECHO through Orthanc.
- [Ready] **Cached health snapshot**: `/api/health` serves the latest in-memory snapshot instead of recomputing remote PACS checks inline on every request, reducing Orthanc and backend overhead.
- [Ready] **System health SSE**: portal and maintenance page react to `GET /api/system/events` so they switch automatically when the backend becomes unavailable or recovers.
- [Ready] **Logs accessible via compose**: `docker compose logs` is the primary inspection path; backend logs are JSON.
- [Ready] **Remote deploy compose log**: `Makefile.deploy.local` persists the output of remote `docker compose up -d --build` to a timestamped log file and prints the path at the end of the deploy.
- [Ready] **Remote tail helpers**: `Makefile.deploy.local` exposes `remote-tail-deploy` for the latest deploy log and `remote-logs` for live `docker compose logs -f`, optionally scoped with `SERVICE=<name>`.
- [Ready] **Remote deploy with volume reset**: `Makefile.deploy.local remote-up` accepts `WIPE_VOLUMES=1` to run `docker compose down -v` before rebuilding, and the same remote deploy log captures both the volume wipe and the subsequent startup.
- [Missing] **Retention automation**: backend cron for Orthanc purge (7 days) + DB cleanup (30 days) not implemented (Milestone 7 pending).
- [Ready] **Real backend portal sessions**: server-side session persistence, expiry validation, and logout invalidation are active for patient and professional flows.
- [Ready] **Viewer/image authorization enforcement**: Stone and DICOMweb access are enforced through backend-issued viewer grants plus Orthanc authorization plugin validation by active session and allowed `StudyInstanceUID`.
- [Open] **Broad remote physician search (`buscar todos`)**: wide remote search flows still need a focused regression pass. Known target behavior when resumed: no unexpected Orthanc `403`, no row/UID drift, and no mismatch between local availability, retrieve, viewer, and download actions for the same study row.
- [Ready] **Viewer access grant handoff**: viewer entry uses short-lived study-scoped grants consumed at portal handoff time before the final viewer redirect.
- [Open] **OHIF integration/lazy-loading strategy**: first confirm whether the current viewer path is only the standard `dicom-web` integration against Orthanc local or whether any Orthanc-specific plugin participates; only then evaluate whether a lazier metadata/loading strategy is relevant.
- [Missing] **SSE proxy correctness**: Nginx config must explicitly disable buffering for SSE routes and set correct headers/timeouts (otherwise intermittent UI failures).
- [Missing] **Operational limits**: explicit concurrency limits and per-job deadlines for retrieve (`max_concurrent_retrieves_global`, per-node limits, retryability states).
- [Missing] **Runbook + troubleshooting**: documented procedures for adding nodes, validating QIDO/auth, retrieve debugging, common Orthanc issues, and expected log/audit entries.
- [Needs Decision] **CI test strategy**: whether integration tests run with a simulated remote Orthanc in compose vs mock handlers only (affects repeatability and scope).

---

## Minimum Decisions Needed Before Coding
1. **Target deployment boundary (beyond localhost)**: whether MVP remains strictly localhost-only or must support LAN/VPN with minimal hardening (TLS, IP allowlist, shared operator key).
2. **DIMSE connectivity model**: confirm if remote PACS can reach Orthanc AE (`4242`) for C-MOVE; otherwise standardize on C-GET where needed.
3. **Legacy support tooling**: dcmtk (containerized) vs Go library for C-FIND/C-MOVE/C-GET where DICOMweb is absent; confirm licensing/ops constraints.
4. **Patient identifier mapping**: whether `PatientID == DNI` is acceptable for MVP environments, or must be configurable / resolved via HIS (ANDES MPI) from day one.
5. **Federated search test environment**: commit to adding a second node (simulated remote Orthanc in compose/CI) to validate SSE + dedup deterministically.
6. **HIS/ANDES auth details**: provide base URL + authentication requirements (even if config-only), to finalize config schema/validation and future-proofing.
