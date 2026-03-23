# Human Decisions Log

Use this file to record the decisions you make after reviewing the agent discussion.

## Status
- In progress

## Decisions
- MVP first, before auth features.
- The first deliverable must be a Docker Compose stack.
- The stack must include `orthanc`, `backend`, `postgres`, `nginx`, and `ohif`.
- Local Orthanc is mandatory and acts as the cache and retrieve target.
- PostgreSQL is the default persistence layer for operational data unless a clearly better fit is justified during the design discussion.
- Orthanc is the source of truth for cached studies; Postgres keeps only an operational index that can be rebuilt.
- No doctor login in the MVP.
- No patient login in the MVP.
- No OTP, sessions, JWT, or share links in the MVP.
- HIS integration is configuration-first in the MVP and patient lookup is not required to be fully operational in the first build slice.
- The first build slice follows HIS Option B: persist and validate HIS configuration only, without executing real Andes MPI patient lookups yet.
- Remote dcm4chee integration details must be externalized as configuration.
- Nginx is exposed only on `http://localhost:8080` for the MVP.
- Nginx is the only public HTTP entrypoint.
- Nginx must proxy only the Orthanc DICOMweb paths needed by OHIF and must not expose Orthanc admin REST endpoints.
- The public landing page is part of the MVP and is served directly by Nginx.
- The landing page brand is `RedImagenesNQN`.
- The landing page should use visual identity inspired by the `andes/app` application.
- The landing page includes a visible patient flow with `Documento + OTP` as a UI-only flow in the MVP.
- The landing page includes a visible physician flow with `DNI / usuario + contraseña` as a UI-only flow in the MVP.
- The landing page and portal-owned UI surfaces must be responsive for mobile devices.
- Physician authentication is still out of MVP implementation scope, but the target future integration is `LDAP provincial + MFA`.
- Patient OTP validation is still out of MVP implementation scope, but the target future integration remains `DNI + OTP`.
- Portal-specific static assets such as logo and favicon must be served independently from OHIF assets.
- OHIF is a viewer surface, not the primary search or access surface.
- Patient access must use a portal-owned study list filtered to authorized patient studies.
- Patient access must not rely on the native OHIF study list.
- Physician access must use a portal-owned search and workflow panel.
- Physician workflow should be asynchronous and must expose remote PACS context, local cache presence, and retrieve state before opening OHIF.
- The native OHIF study list is a UX choice only and must not be treated as an access-control mechanism.
- Future real access control must be enforced by backend/proxy using active portal session and allowed `StudyInstanceUID`, not by viewer visibility rules alone.
- The explicit patient and physician UI contracts live in `artifacts/05_ui_contracts.md`.
- The initial remote dcm4chee node uses `AE Title = PACSHPN`.
- The initial remote dcm4chee node uses DICOM port `11112`.
- The initial remote dcm4chee node supports `C-MOVE`.
- The dcm4chee Swagger / OpenAPI specification is the REST integration contract for the MVP.
- The MVP uses dcm4chee `QIDO-RS`, `WADO-RS`, and `MOVE` capabilities only.
- dcm4chee PAM-RS and patient administration operations are out of the first slice.
- Remote dcm4chee REST authentication uses Keycloak `client_credentials`.
- The backend must request an access token using `client_id` and `client_secret` from environment configuration.
- The backend must call the remote PACS REST API with `Authorization: Bearer <token>`.
- The Andes MPI patient lookup candidate endpoint is `GET /api/core-v2/mpi/pacientes?documento=<dni>`.
- If Andes MPI returns multiple matches, the backend should refine using `fechaNacimiento`, `sexo`, `apellido`, or `nombre`.
- The MVP includes a minimal web UI served by Nginx: search form, streaming results, Retrieve action, and Visualizar link.
- Search result streaming uses SSE, not WebSocket.
- The minimum MVP search fields are `patient_id`, `patient_name`, `date_from`, `date_to`, and `modalities`.
- If a remote node cannot support a given filter, the backend should still query the node with supported filters and mark the result as partially filtered.
- The operator result list must show `PatientName`, `PatientID`, `StudyDate`, `StudyTime`, `ModalitiesInStudy`, `StudyDescription`, `source nodes`, and `cache status`.
- Deduplicated study metadata should prefer the highest-priority remote node; cache state is shown separately from study metadata.
- The Visualizar action is enabled only after the retrieve job reaches `done`.
- Retrieve is manual-only in the MVP.
- The MVP must support both `C-MOVE` and `C-GET`.
- `C-MOVE` is initiated against the remote PACS, targeting local Orthanc as the destination AE.
- `C-GET` is triggered through the local Orthanc REST API, so Orthanc acts as the SCU against the remote modality and receives the returned instances.
- `C-MOVE` remains the preferred retrieve path when available.
- Retrieve completion uses Orthanc polling with a stable window and global timeout.
- Backend and database logs must minimize PHI; avoid storing patient names and DNI in integration logs unless strictly required for operation.
- Operational tables such as audit and search execution records should be cleaned up automatically with a default retention of 30 days.
- Orthanc cache retention remains 7 days.
- DIMSE tooling for MVP should live in the backend container to keep the first Compose stack simpler.
- HTTP only on localhost is acceptable for the MVP; TLS is deferred to staging or on-prem deployment.
- Orthanc DICOM port `4242` is published in local development environments where remote PACS reachability is required for `C-MOVE`.
- Orthanc direct HTTP and DICOM host ports should default to localhost binding in local development when possible.
- `dcmtk` is the accepted DIMSE tooling choice for the MVP backend container.
- The SSE contract uses event types `node_started`, `study`, `node_finished`, `error`, and `done`.
- SSE allows interleaved cross-node events, but event ordering must be preserved per node.
- Each search stream must emit exactly one terminal `done` event.
- The SSE client retry interval defaults to 3000 ms.
- For lab or CI integration testing, a simulated remote Orthanc node is preferred over mock-only handlers when feasible.
- OHIF is pinned to `ohif/app:v3.11.1` instead of using `latest`.
- OHIF must read the local cache through `/dicom-web/`.
- OHIF study list must remain enabled in local operation.
- For final patient and physician flows, the native OHIF study list should be disabled unless explicitly needed for a technical operator surface.

## Open Inputs From User
- HIS base URL, authentication method, API key format, and required query parameters.
- Remote dcm4chee nodes: hostnames, DICOMweb base URLs, REST credentials, and Keycloak host details.
- Andes HIS base URL in the target environment.
- Confirmation of the exact auth flow for Andes in the target environment.

## Questions To Revisit During Spec
- Which exact Orthanc REST endpoints and payloads will be used for `C-GET` orchestration in the implementation.

## Notes For Agents
- Prefer pragmatic, secure defaults.
