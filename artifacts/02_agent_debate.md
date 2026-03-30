# Design Review — Portal DICOM Agregador + Caché (Orthanc) + OHIF (MVP)

## Product Review

### What’s good
- **The current auth boundary is materially stronger than the original MVP**:
  - backend sessions already exist for patient and physician flows;
  - viewer access already uses short-lived grants;
  - Orthanc authorization now participates in the viewer/image path.
- **The public landing page is already useful** even without real auth: it gives the project a credible entry point and makes the portal feel like a product instead of just an integration stack.
- **Brand alignment with ANDES** is a good product decision for user familiarity in Neuquén.
- **Mobile responsiveness is the right requirement early** because both patient and physician entry flows are likely to be opened from phones.
- **User-perceived performance is addressed** by enforcing “OHIF reads only from local Orthanc cache,” decoupling viewer latency from remote PACS.
- **Separating portal workflow from OHIF viewer responsibilities** is the right product move for both patient and physician journeys.
- **Concurrent search + streaming results (SSE/WS)** is aligned with “fast feedback” UX for multi-node queries.
- **Dedup by `StudyInstanceUID` with `locations[]`** is the right abstraction for a federated PACS experience.
- **Retrieve is explicit (button / endpoint)** which keeps behavior predictable for early integrations and testing.
- **Contrato inicial de UI paciente y médico** ya existe en el código y permite evolucionar sin bloquearse por auth real:
  - `GET /api/patient/studies`
  - `GET /api/physician/results`
  - `POST /api/patient/retrieve`
  - `POST /api/physician/retrieve`
- **The non-viewer API boundary is now aligned with the session model**:
  - patient and physician protected routes no longer trust caller-supplied `document_number` / `username`,
  - which removes the main bypass around the viewer authorization work.

### Risks / ambiguities
- **There are now two UI surfaces with different purposes**:
  - a public landing page with patient/professional entry flows,
  - an operational search/retrieve/view workflow.
  The boundary between them is not yet formally specified.
- **The role of OHIF vs portal must be explicit**:
  - if OHIF keeps a native study list enabled, patients could conceptually “see everything” in the local cache;
  - hiding the list in OHIF is useful for UX, but not enough as a product-level access design.
- **The public landing shows future auth concepts** (`código por mail`, `LDAP provincial`, `MFA`) that are intentionally not implemented in the MVP. This must be stated clearly in specs and acceptance criteria to avoid stakeholder confusion.
- **Patient auth is still transitional in one key dimension**:
  - `mail` is the intended final production mode,
  - but real mail delivery and one-time-code verification are still pending,
  - so `master_key` remains a temporary operational bypass that must stay clearly labeled as such.
- **Search semantics across nodes are not normalized**:
  - Differences in remote QIDO behavior (fuzzy name matching, date handling, character sets) can yield inconsistent results.
  - Legacy C-FIND query capabilities may not match QIDO filters (e.g., modality/date ranges).
- **Definition of “study ready to view”** may be confusing if retrieve completes but images are still arriving; the “stable window polling” is pragmatic but can lead to perceived flakiness.
- **Dedup conflict resolution** (metadata discrepancies across nodes) is not defined (e.g., PatientName differs; which one is displayed?).
- **HIS integration is “config only”** but already includes specific Andes MPI endpoint behavior in the decision log; risk of scope creep if stakeholders expect it to work in MVP.
- **Definición de “autorizado” para pacientes en el MVP**: hoy el primer slice usa `PatientID=<dni>`. En muchos entornos `PatientID` no es DNI, sino historia clínica u otro identificador local. Esto puede romper la UX futura del paciente si se consolida demasiado temprano como contrato implícito.
- **Búsqueda profesional real todavía es parcial**:
  - con filtros ya hace QIDO real al nodo remoto configurado;
  - sin filtros todavía depende de recientes persistidos como fallback.
  Eso sirve para MVP, pero no representa aún la experiencia final esperada por un médico.

### Concrete decisions the human should make next
1. **Mail auth completion plan**: define provider, OTP TTL, resend limits, retry policy, and lockout semantics so `patient.auth_mode = "mail"` can replace the temporary `master_key` path operationally.
2. **Portal surface split**: confirm whether the landing page and the operator search UI are the same surface or two separate routes/views.
3. **Patient viewer model**: confirm that patients use a portal-owned filtered study list and never the native OHIF study list.
4. **Physician workflow model**: confirm that physicians use a portal-owned asynchronous search panel, not the native OHIF study list.
5. **Minimum physician panel fields**: define the exact remote PACS metadata shown in results (node, availability, retrieve state, local cache presence, last sync/latency if needed).
6. **Minimum search filters to support in MVP** across QIDO and C-FIND (dates, modalities, patient_id, patient_name) and what to do when a node can’t support a filter.
7. **Patient identifier strategy**: confirm whether the MVP can safely assume `PatientID == DNI` in the current environment, or whether the contract must become configurable now (`PatientID`, issuer-aware lookup, or HIS/MPI first).

---

## Security Review

### What’s good
- **Single HTTP ingress via Nginx** and internal service networking is a strong baseline for MVP containment.
- **Portal assets are now separated from OHIF assets**, which reduces accidental routing collisions and makes static serving easier to reason about.
- **Secrets externalized** (env vars / mounted files) and “secret refs” in config is the right pattern.
- **Keycloak client_credentials flow** for dcm4chee REST is explicitly defined (token retrieval + bearer usage).
- **Technical audit logging** is included early, which helps incident triage and integration debugging.
- **Principio correcto sobre seguridad**: esconder o deshabilitar la study list de OHIF no se considera control de acceso; esa frontera queda explícitamente reservada para el portal/backend.
- **No persistir credenciales en claro** ya quedó asentado como regla de implementación.

### Risks / ambiguities
- **MVP has no end-user auth**, but the system can still expose PHI if reachable beyond a controlled network. The spec relies on an assumption (“LAN/VPN”), but hard controls are not fully spelled out.
- **Nginx path exposure needs tightening**:
  - There are now multiple route families (`/`, `/ohif/`, `/dicom-web/`, `/dicomweb/`, `/portal-assets/`, root OHIF bundles) and they must stay intentionally partitioned.
  - Orthanc REST admin endpoints must not be reachable through the proxy.
- **UX-level hiding is not security**:
  - disabling OHIF study list is not a substitute for controlling which studies are exposed by the portal/backend;
  - patient lists must be portal-authored and authorization-aware.
- **Viewer protection still needs a real future boundary**:
  - the core viewer boundary now exists,
  - but browser-origin controls and non-viewer API protections must continue evolving together with the same session-based model.
- **Orthanc DICOM port exposure (4242)**: opening it to broader networks can allow unauthorized C-STORE into cache unless constrained (AE whitelist, firewall rules, TLS, or at least network segmentation).
- **Token handling**:
  - Where and how tokens are cached, TTL handling, refresh on 401, and avoiding logging tokens are not explicit.
- **Audit content**: logging patient identifiers (patient_id, name, DNI) is sensitive; MVP says “technical audit,” but it will likely capture PHI unless explicitly minimized/redacted.
- **MVP sin auth pero con endpoints operativos reales**:
- **State-changing browser routes need CSRF posture to stay explicit**:
  - current implementation now enforces a basic same-origin `Origin`/`Referer` check on unsafe browser methods,
  - but a future token-based CSRF design may still be desirable if the app expands beyond the current deployment assumptions.
- **MVP sin auth pero con endpoints operativos reales**:
  - `/api/patient/studies`
  - `/api/patient/retrieve`
  - `/api/physician/results`
  - `/api/physician/retrieve`
  pueden ser abusados en redes no-localhost si no hay controles mínimos de exposición.
- **DICOMweb local sin política de exposición suficientemente explícita**:
  - aunque OHIF necesita acceder a `/dicom-web/*`,
  - el portal público no necesariamente debería dejar ese surface libre en cualquier entorno más allá de localhost.

### Concrete decisions the human should make next
1. **Network exposure rule for MVP**: confirm “only localhost” vs “LAN/VPN” and required firewalling expectations.
2. **Nginx allowlist for Orthanc**: which exact Orthanc endpoints are proxied (`/dicom-web/*` only) and deny everything else.
3. **Public route contract**: confirm that `/portal-assets/` is the reserved namespace for portal-owned static assets.
4. **Orthanc hardening baseline**:
   - Enable authentication for Orthanc REST even internally? (recommended if any risk of lateral access)
5. **Actor UI contracts**:
   - freeze the patient and physician screen contracts in dedicated artifacts before wiring real auth and retrieve UX.
   - AE Title checks / known modalities / known remote AEs.
6. **Logging policy**: what PHI is permitted in logs and Postgres tables for MVP; define redaction/hashing rules now to avoid rework later.
7. **Minimum control for non-localhost environments**: decide whether MVP should require at least one of:
   - `X-Portal-Key` shared header,
   - IP allowlist,
   - Basic Auth at Nginx,
   for `/api/*` and `/dicom-web/*` outside pure localhost.

---

## Operations Review

### What’s good
- **Compose-based “one command up”** with volumes and migrations is a clear operational goal for repeatable dev/test environments.
- **Separation of concerns** is sensible: Orthanc for image storage + DICOMweb, Postgres for jobs/config/audit, Go backend for orchestration.
- **Pinning OHIF to `ohif/app:v3.11.1`** is an operational improvement over `latest`.
- **Explicit retention policy** (7 days + max disk) and a purge mechanism is included, preventing uncontrolled disk growth.
- **An asynchronous physician workflow** fits the reality of remote PACS latency and retrieve timing much better than trying to use OHIF as the primary search tool.
- **SSE recommended over WS** simplifies reverse proxying, scaling, and troubleshooting.
- **Logs estructurados en vez de métricas en Postgres** es una buena decisión inicial para no contaminar la base operacional.

### Risks / ambiguities
- **Retrieve completion detection** via “stable window” polling can be noisy operationally:
  - Large studies may have long gaps; false “complete.”
  - Retries and timeouts need careful tuning per modality/network.
- **DIMSE tooling in containers**: if using dcmtk CLI, the image size and OS deps increase; also need consistent error parsing and robust timeouts.
- **Observability gaps**:
  - No explicit metrics (queue depth, retrieve durations, per-node latency, Orthanc storage growth).
  - Logs exist, but without dashboards/alerts troubleshooting will be manual.
- **Data model size growth**: `integration_audit` and `search_*` tables can grow quickly even in MVP; need retention/cleanup strategy.
- **Orthanc purge implementation choice**: backend cron is controllable, but must be robust (idempotent, safe deletes, handles in-progress retrieve).
- **Red y reachability DIMSE**:
  - C-MOVE requiere que el PACS remoto alcance el Orthanc local (`4242`);
  - si eso no está garantizado, el sistema puede funcionar solo con `C-GET` en algunos entornos.
- **Rutas DICOMweb duplicadas**:
  - coexistencia de `/dicomweb` y `/dicom-web` complica documentación, debugging y configuración OHIF/Nginx.
- **Faltan límites operativos explícitos**:
  - concurrent retrieves por nodo/global,
  - timeouts estándar por job,
  - comportamiento cercano a disco lleno.

### Concrete decisions the human should make next
1. **How retrieves are executed**: dcmtk CLI inside backend container vs separate “dicom-tools” sidecar container.
2. **Operational retention for Postgres logs/audit**: e.g., keep 7/30/90 days; purge job.
3. **Basic metrics**: decide minimal MVP instrumentation (Prometheus endpoints? at least structured logs + key counters).
4. **Backup expectations**:
   - Postgres volume backup needed for MVP?
   - Orthanc cache is ephemeral by design; confirm no backup required.
5. **Resource limits**: default CPU/mem constraints in Compose and maximum concurrent retrieves/search fanout to protect local machine.
6. **Canonical DICOMweb route**: decide one final route (`/dicom-web/` recommended) and normalize code, docs and Nginx around it.
7. **Preferred retrieve default by environment**: decide whether `C-MOVE` remains the default when networking allows it, with per-node fallback to `C-GET`.

---

## Decision Proposals

1. **Decision name:** MVP UI Scope  
   **Recommended option:** Keep **two explicit UI surfaces** in the spec:
   - public landing page on `/`
   - operational search/retrieve/view workflow behind its own route/view.
   **Why this option is recommended:** It matches the implementation already in progress and avoids mixing public entry UX with technical operator workflows.  
   **Alternatives to consider:**  
   - Merge both concerns into one single page.
   - API-only + Postman collection + documented OHIF deep-link (fastest engineering).

2. **Decision name:** Streaming Protocol for Search Results  
   **Recommended option:** **SSE** for MVP.  
   **Why this option is recommended:** Works well behind Nginx, simpler than WebSockets, unidirectional fits “server pushes partial results.”  
   **Alternatives to consider:**  
   - WebSockets if you need cancelation, bidirectional control, or richer interaction.  
   - Long polling (simpler but worse UX and higher overhead).

3. **Decision name:** Orthanc Exposure Through Nginx  
   **Recommended option:** Proxy **only DICOMweb paths** needed by OHIF under `/dicom-web/...`, keep `/dicomweb/...` only as a compatibility alias, and explicitly **deny** Orthanc admin REST endpoints.  
   **Why this option is recommended:** Reduces accidental exposure of privileged Orthanc functionality while preserving viewer operation.  
   **Alternatives to consider:**  
   - Put Orthanc behind its own auth even internally (stronger but more setup).  
   - Allow full Orthanc REST temporarily in dev (faster debugging, higher risk).

4. **Decision name:** Retrieve Mechanism per Node  
   **Recommended option:** Default to **C-MOVE**, with per-node fallback to **C-GET** configurable.  
   **Why this option is recommended:** Matches the current architecture decision, aligns with typical PACS routing, and still leaves a practical escape hatch for hostile network topologies.  
   **Alternatives to consider:**  
   - Only C-MOVE in MVP (simplest; may fail in some sites).  
   - Only C-GET (avoids inbound connectivity to Orthanc but changes networking assumptions).

5. **Decision name:** Retrieve Completion Criteria  
   **Recommended option:** Use **polling with “stable window”** + global timeout, but also record a “possibly_incomplete” terminal state if stability is reached too quickly or instance count is very low.  
   **Why this option is recommended:** Keeps MVP deterministic while reducing false positives and supporting operational debugging.  
   **Alternatives to consider:**  
   - Determine expected instance count from remote metadata (more complex; not always reliable).  
   - Integrate Orthanc/DICOM receive logs/events if available (more invasive).

6. **Decision name:** Logging & PHI Handling in MVP  
   **Recommended option:** Implement a **PII/PHI-minimizing logging policy** now: store identifiers in operational tables only when required; redact/hash patient_name/DNI in `integration_audit` and application logs.  
   **Why this option is recommended:** Prevents accidental PHI spillage into logs (which are routinely copied/shared), while still enabling troubleshooting.  
   **Alternatives to consider:**  
   - Log everything in dev only (fastest, but risky and hard to unwind).  
   - Full PHI logging with strict access controls (overkill for MVP, conflicts with “no auth”).

7. **Decision name:** Postgres Retention for Operational Tables  
   **Recommended option:** Add a **DB cleanup job** (backend cron) for `integration_audit`, `search_requests`, `search_node_runs` (e.g., keep 30 days by default, configurable).  
   **Why this option is recommended:** Prevents unbounded growth and keeps the MVP operable over time.  
   **Alternatives to consider:**  
   - No cleanup in MVP (acceptable only for very short-lived dev).  
   - Time-partitioned tables (more complex, better for long-term scale).

8. **Decision name:** Containerization of DIMSE Tools (Legacy / Retrieve)  
   **Recommended option:** Use a **sidecar container** (e.g., `dicom-tools`) that includes dcmtk, invoked by backend via exec/HTTP RPC.  
   **Why this option is recommended:** Keeps the Go backend image smaller/cleaner, isolates OS dependencies, and simplifies future replacement (library vs CLI).  
   **Alternatives to consider:**  
   - Install dcmtk directly in backend container (simpler wiring, bigger image).  
   - Use a Go native DIMSE library (less shelling out, but higher integration risk).

9. **Decision name:** TLS for MVP Entry Point  
   **Recommended option:** **HTTP only on localhost for MVP** (per decision log `http://localhost:8081`), but document a **staging/on-prem TLS profile** (Nginx terminates TLS) as a follow-up.  
   **Why this option is recommended:** Matches the current human decision and avoids certificate friction, while keeping a clear path for secure deployment.  
   **Alternatives to consider:**  
   - Enable TLS immediately with self-signed certs (more realistic, more overhead).  
   - Terminate TLS upstream (load balancer) and keep Nginx HTTP internally.

10. **Decision name:** Source of Truth for “Cached Study Index”  
   **Recommended option:** Treat **Orthanc as the truth**, with Postgres `cached_studies` as an **operational index** that can be rebuilt (best-effort).  
   **Why this option is recommended:** Avoids split-brain; if Postgres is wrong, you can re-verify against Orthanc.  
   **Alternatives to consider:**  
   - Postgres as truth and Orthanc as opaque store (harder, fragile).  
   - No cached index table at all (simpler, slower UX and more Orthanc calls).

11. **Decision name:** Patient Identifier Strategy for MVP  
   **Recommended option:** Make the patient lookup field/tag **configurable** rather than hardcoding `PatientID == DNI`.  
   **Why this option is recommended:** It prevents the MVP contract from ossifying around an assumption that often fails in real PACS deployments, while preserving a path to HIS/MPI-backed resolution later.  
   **Alternatives to consider:**  
   - Assume `PatientID == DNI` in the current environment and revisit later.  
   - Force HIS/MPI integration before exposing patient search at all.

12. **Decision name:** Minimum Controls for Non-Localhost MVP  
   **Recommended option:** For any environment beyond pure localhost, require at least one lightweight control for `/api/*` and `/dicom-web/*`:
   - shared header key,
   - IP allowlist,
   - or Basic Auth at Nginx.  
   **Why this option is recommended:** It preserves the “no end-user auth” scope while avoiding trivial enumeration and retrieve abuse in semi-open networks.  
   **Alternatives to consider:**  
   - No control at all outside localhost.  
   - Full end-user auth immediately (out of scope for MVP).
