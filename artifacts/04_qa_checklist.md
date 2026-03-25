# MVP QA Gap Checklist — Portal DICOM Agregador + Caché (Orthanc) + OHIF

## Security
- [Ready] **Single public HTTP entrypoint**: only Nginx exposed on `http://localhost:8081`; backend/Orthanc HTTP not directly exposed.
- [Ready] **Orthanc admin REST not exposed**: Nginx denies non‑DICOMweb Orthanc routes (explicit 403/404).
- [Ready] **DICOMweb-only viewer data path**: OHIF configured to consume only `/dicom-web/` (Orthanc local) and never remote PACS.
- [Ready] **Secrets not committed**: config uses `*_secret_ref` / env/file refs; runtime `config.json` is local-only and ignored.
- [Ready] **No auth in MVP**: no sessions/JWT/email-code validation implemented; UI flows are visual only.
- [Missing] **Hardening for non-localhost environments**: minimal protections when deployed beyond localhost (TLS termination, IP allowlist, shared operator key, etc.).
- [Missing] **Token handling hardening**: Keycloak token cache/refresh policy (TTL, refresh-on-401) + explicit guarantee tokens/secrets never appear in logs.
- [Missing] **PHI minimization policy enforcement**: explicit list of fields allowed in `integration_audit`, `search_*` tables, and logs (and automated checks).
- [Needs Decision] **Network exposure of DIMSE ports**: confirm when/where Orthanc DICOM `4242` is published and reachable from remote PACS (VPN/NAT/firewall model).

## Functional
- [Ready] **Landing + portal surfaces**: patient/professional mock flows route to portal-owned pages first; responsive requirement captured.
- [Ready] **Patient list contract**: `GET /api/patient/studies?document=<dni>` returns `200` with `studies: []` if none; “Actualizar lista” semantics defined.
- [Ready] **Patient async search worker**: remote patient search runs through `search_requests`/`search_node_runs`, started by `POST /api/patient/search`, polled through `GET /api/patient/search?request_id=...`, with visible `queued/running` state in the patient results surface.
- [Ready] **Manual retrieve contract**: `POST /api/patient/retrieve`, `POST /api/physician/retrieve`, job persistence and state transitions exist (queued→running→done/failed).
- [Ready] **Viewer handoff**: portal opens `GET /ohif/viewer?StudyInstanceUIDs=<uid>` in a new tab; Visualizar enabled only when local cache is ready.
- [Ready] **OHIF root containment**: `GET /ohif/` redirects to landing and patient/professional flows enter OHIF only through study-specific viewer URLs.
- [Ready] **Retrieve completion heuristic**: Orthanc polling with stable window + global timeout defined.
- [Ready] **Hospital/source labeling in results**: patient and professional result cards show the configured PACS node display name (`pacs_nodes.name`) as the hospital/sede label instead of exposing only technical node identifiers.
- [Ready] **Professional PACS health legend**: the physician results card shows `PACS en línea X/Y` from remote PACS health checks and exposes online/offline node names on hover/focus.
- [Ready] **Professional license exceptions by config**: `professional.license_exceptions` can authorize a bounded list of DNI/username entries without active matrícula while preserving the `habilitado == true` check.
- [Ready] **First LDAP professional login slice**: when `professional.fake_auth = false`, `POST /api/physician/login` authenticates against LDAP using `LDAP_HOST`, `LDAP_PORT`, `LDAP_OU` and direct `uid=<dni>,<LDAP_OU>` bind before applying Mongo-based authorization.
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
- [Ready] **System health SSE**: portal and maintenance page react to `GET /api/system/events` so they switch automatically when the backend becomes unavailable or recovers.
- [Ready] **Logs accessible via compose**: `docker compose logs` is the primary inspection path; backend logs are JSON.
- [Missing] **Retention automation**: backend cron for Orthanc purge (7 days) + DB cleanup (30 days) not implemented (Milestone 7 pending).
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
