## QA Readiness Checklist (Pre-coding)

### Security
- [Ready] Nginx is the **only** public HTTP entrypoint; backend/Orthanc HTTP/Postgres remain internal-only.
- [Ready] Nginx exposes HTTP only on `http://localhost:8080` for MVP; TLS deferred.
- [Ready] Portal-owned static assets can be served from a dedicated route namespace independent from OHIF assets.
- [Missing] Nginx **explicit allowlist** of DICOMweb paths required by OHIF, with explicit deny for Orthanc admin REST (confirm exact path list and add negative tests).
- [Missing] Nginx proxy hardening defaults: timeouts, max body size, upstream keepalive, request buffering behavior for SSE, and basic security headers (even on LAN/VPN).
- [Ready] Secrets not stored in images/repo; configuration uses `*_secret_ref` resolved from env/file.
- [Ready] The database model forbids storing physician passwords in clear text; only session state, auth events, and encrypted provider-issued auth material are allowed.
- [Missing] Backend log redaction policy implemented and tested (no `client_secret`, no bearer tokens; minimize PHI in logs/audit).
- [Missing] CORS policy at Nginx/backend for UI/OHIF origins (even if same-origin, document and enforce).
- [Needs Decision] Whether Orthanc DICOM port `4242` must be published in *all* environments or only where remote PACS reachability exists (impacts exposure and firewall rules).

### Functional
- [Ready] Public landing page is part of the MVP surface and is served by Nginx.
- [Ready] Public landing reflects current UX decisions: patient `Documento + OTP` and physician `DNI / usuario + contraseña`.
- [Ready] Current mock login flows land on portal-owned mock surfaces before opening OHIF for a specific study.
- [Ready] Physician future target auth is documented as `LDAP provincial + MFA`, but remains out of MVP implementation scope.
- [Ready] Mobile responsiveness is an explicit requirement for the landing and portal-owned surfaces.
- [Ready] OHIF is treated as a viewer surface, not the primary patient or physician search surface.
- [Ready] Patient portal study list contract is documented in `artifacts/05_ui_contracts.md`, including fields, sort, filters, actions, and availability states.
- [Ready] Physician panel contract is documented in `artifacts/05_ui_contracts.md`, including filters, columns, states, and actions.
- [Ready] Relational model for patient cache, HIS alternate identifiers, known study UIDs, physician recent searches, and auth/session state is documented in `artifacts/06_data_model.md`.
- [Ready] API surface defined for health, config, search (SSE), retrieve jobs, cache status.
- [Ready] Search streaming uses SSE (not WS); UI renders incremental results.
- [Missing] Concrete SSE event schema contract (field names, ordering guarantees, retry behavior, terminal `done`, and error payload structure).
- [Ready] Deduplication key and merge policy defined (`StudyInstanceUID`, prefer highest-priority node, accumulate `locations[]`, separate cache state).
- [Missing] Definition of “partial filter” flags: exact fields `partial_filter`, `unsupported_filters[]`, and how UI displays them.
- [Ready] Retrieve job state machine defined (`queued→running→done/failed`) and persisted.
- [Ready] Retrieve completion via Orthanc polling with stable window + global timeout.
- [Missing] Precise “stable window” parameters: poll interval, stable duration, max timeout defaults, and how to handle zero-instance/stuck edge cases.
- [Ready] Cache retention 7 days; DB operational retention 30 days via backend cron.

### Integration
- [Ready] Remote dcm4chee REST auth uses Keycloak `client_credentials` (token cached with TTL; refresh on 401).
- [Needs Decision] Provide real integration inputs for at least one remote node: dcm4chee hostname(s), DICOMweb base URL, Keycloak token URL/realm, `client_id`, and how secrets are mounted in the compose environment.
- [Ready] Remote DIMSE basics captured (initial node AE Title `PACSHPN`, port `11112`, supports C-MOVE).
- [Needs Decision] DIMSE network topology confirmation: remote PACS can reach **local Orthanc** as Move SCP on `4242` (routing/NAT/firewall/VPN specifics).
- [Missing] Decide and document DIMSE toolchain inside backend container (dcmtk binaries + exact commands, exit-code mapping, and log parsing strategy).
- [Needs Decision] Orthanc REST orchestration for C-GET: exact endpoints/payloads to trigger C-GET and expected responses (blocking for Milestone 5).
- [Ready] OHIF consumes only local Orthanc via `/dicom-web` proxied by Nginx, with `/dicomweb` retained only as compatibility alias if needed.
- [Ready] OHIF image is pinned (`ohif/app:v3.11.1`) instead of using `latest`.
- [Missing] Final OHIF mode configuration for actor-specific flows (study list disabled for patient and physician surfaces once portal-owned lists exist).
- [Missing] OHIF configuration artifact committed/templated for environment-specific base URLs and routes (and validated that no remote DICOMweb endpoints can be configured).
- [Ready] Future access-control requirement is explicit: backend/proxy must validate active session + allowed `StudyInstanceUID` for viewer/image access; hidden OHIF UI is not sufficient.

### Operability
- [Ready] One-command `docker compose up` is a hard acceptance criterion.
- [Ready] Branding/static assets are part of the runtime contract and should be included in smoke verification for `/` and favicon delivery.
- [Missing] Deterministic startup ordering/healthchecks (postgres ready → migrations → backend ready; orthanc ready → backend health returns `orthanc_ok`).
- [Ready] Database migrations automation is defined and implemented in the backend startup using versioned SQL files plus `schema_migrations`.
- [Missing] Observability baseline: structured JSON logs for backend + where logs live; minimal metrics (latency per node run, retrieve durations) persisted and/or exposed.
- [Missing] Runbook content list and ownership (how to add PACS nodes, test search/retrieve/view, troubleshoot common failures).
- [Missing] Automated smoke tests for compose: health endpoint, SSE contract, proxy negative tests (Orthanc admin blocked), basic retrieve state machine (even against lab/mock).
- [Needs Decision] Test strategy for “remote PACS not available” in CI: include a lab Orthanc/dcm4chee container as a simulated remote vs. mock handler only.

---

## Minimum Decisions Needed Before Coding
1. **Nginx DICOMweb allowlist**: confirm the exact Orthanc DICOMweb paths OHIF needs (to implement allow/deny + negative tests).
2. **DIMSE reachability**: confirm whether remote PACS can reach local Orthanc `AE/host/port` for C-MOVE in the target MVP environment (firewall/NAT/VPN specifics).
3. **C-GET orchestration contract**: confirm the exact Orthanc REST endpoints/payloads to initiate C-GET (and required Orthanc config for remote modalities).
4. **Integration credentials delivery**: provide Keycloak token endpoint/realm + dcm4chee DICOMweb base URL(s) + how `client_id/client_secret` are supplied (env vs mounted file) for dev/testing.
5. **Tooling choice for DIMSE**: confirm dcmtk is acceptable inside the backend container (package source/version) and whether any licensing/compliance constraints apply.
6. **SSE contract finalization**: confirm event types/payload fields and client retry behavior (so UI + backend + tests align).
7. **Future auth contract for physicians**: confirm the provincial LDAP integration boundary and MFA factor type to avoid later UX rework in the public landing.
8. **Patient study-list contract**: confirm what exact metadata and filtering the patient list should expose.
9. **Physician panel contract**: confirm the exact operational metadata to expose for remote PACS, availability, and retrieve state.
