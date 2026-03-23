# Implementation Plan (MVP) — Portal DICOM Agregador + Caché Local + OHIF

## Milestone 1 — Compose stack + reverse proxy boundary
**Goal**: Levantar el “one command up” con la frontera HTTP única en Nginx, branding base y servicios internos cableados.

**Deliverables**
- `docker-compose.yml` con servicios: `nginx`, `backend`, `postgres`, `orthanc`, `ohif`.
- Redes separadas (`frontend_net`, `backend_net`) y volúmenes (`postgres-data`, `orthanc-db`, `orthanc-storage`).
- Config Nginx:
  - `/:` landing pública
  - `/api:` proxy a backend
  - `/ohif:` proxy a OHIF
  - `/dicom-web:` proxy **solo** a endpoints DICOMweb de Orthanc (deny explícito al resto)
  - `/dicomweb:` alias de compatibilidad a `/dicom-web/`
  - `/portal-assets:` assets estáticos propios del portal
- Backend “skeleton” (Go) con `/api/health` y logs JSON.
- Orthanc configurado (DICOM + DICOMweb) accesible **solo** vía Nginx HTTP; DICOM port 4242 publicado según compose (LAN).
- OHIF configurado y pinneado a `ohif/app:v3.11.1`, consumiendo DICOMweb en `/dicom-web` (a través de Nginx).
- Landing pública con branding `RedImagenesNQN`, assets propios y referencia visual ANDES.
- Landing pública responsive para dispositivos móviles.

**Dependencies**
- Ninguna externa (no requiere PACS remoto real).

**Exit criteria (testable)**
- `docker compose up` levanta todo sin pasos manuales.
- `GET http://localhost:8080/api/health` ⇒ 200 y reporta `db_ok` + `orthanc_ok`.
- `GET http://localhost:8080/` carga la landing pública con logo y favicon propios.
- `GET http://localhost:8080/ohif/` carga UI de OHIF.
- Verificación negativa: URLs de Orthanc admin REST (p.ej. `/instances`, `/studies` si no son DICOMweb) **no** son accesibles vía Nginx (403/404), mientras que DICOMweb sí (p.ej. `/dicom-web/studies` responde según Orthanc).

---

## Milestone 2 — Persistencia + migraciones + configuración externalizada
**Goal**: Tener Postgres operativo con migraciones, modelo mínimo, y carga/validación de `config.json` + secretos por env/file refs.

**Deliverables**
- Migraciones versionadas (p.ej. golang-migrate):
  - `pacs_nodes`, `his_config`, `patients`, `patient_identifiers`, `patient_sessions`, `patient_study_access`
  - `physicians`, `physician_sessions`, `physician_recent_queries`, `auth_events`, `auth_material_cache`
  - `search_requests`, `search_node_runs`, `retrieve_jobs`, `cached_studies`, `integration_audit`
- Repositorios Go (storage layer) para CRUD mínimo:
  - read config (`pacs_nodes`, `his_config`)
  - create/update `search_requests`, `search_node_runs`, `retrieve_jobs`, audit
  - upsert de identidad paciente, identificadores alternativos y estudios autorizados
  - persistencia de búsquedas recientes y sesiones de profesional
- Loader de `CONFIG_PATH` (JSON) al arranque:
  - upsert de `pacs_nodes` y `his_config`
  - validación de schema + “secret_ref resolution” desde env o archivo montado
- Endpoint `GET /api/config` (read-only en MVP inicial; `PUT` opcional posterior).

**Dependencies**
- Milestone 1 (stack levantado y conectividad backend↔postgres).

**Exit criteria (testable)**
- Arranque del backend ejecuta migraciones automáticamente.
- Cambiar `config.json` (p.ej. nombre/priority de nodo) se refleja en `GET /api/config`.
- Logs y `integration_audit` no incluyen `client_secret` ni tokens (verificación manual en logs + DB).
- Contraseñas de médicos y OTPs no se persisten en claro en ninguna tabla.

---

## Milestone 3 — Landing pública + búsqueda agregada con SSE + deduplicación
**Goal**: Consolidar la landing pública y la búsqueda concurrente multi-nodo con streaming SSE, dedup por `StudyInstanceUID`, y UI mínima para probar el loop.

**Deliverables**
- API:
  - `POST /api/search` → crea `search_id`, persiste `search_requests`
  - `GET /api/search/{id}/events` (SSE): `node_started`, `study`, `node_finished`, `error`, `done`
- Engine de búsqueda:
  - Worker pool por search; timeout por nodo (`timeout_ms`)
  - `DICOMHandler` con al menos:
    - `QIDORSHandler` (HTTP QIDO-RS) con auth Keycloak client_credentials
    - `LocalCacheHandler` (consulta Orthanc DICOMweb/QIDO local para “cache hit”)
  - “Partial filtering”: si un nodo no soporta filtro, ejecutar con lo soportado y marcar bandera en resultado (p.ej. `partial_filter=true`, `unsupported_filters=[...]`).
- Deduplicación:
  - clave `StudyInstanceUID`
  - metadatos preferidos: nodo de mayor prioridad
  - `locations[]` acumulado + `cache.present`
- UI estática (Nginx):
  - Landing pública:
    - selector `Paciente` / `Profesional`
    - flujo visual `Documento + OTP`
    - flujo visual `DNI / usuario + contraseña`
    - textos alineados con roadmap `LDAP provincial + MFA` para médicos
    - aclaración visual de que el portal es la superficie de acceso y OHIF es el visor
    - adaptación responsive para teléfonos y tablets
  - Form con campos MVP: `patient_id`, `patient_name`, `date_from`, `date_to`, `modalities`
  - Tabla incremental con columnas requeridas: `PatientName`, `PatientID`, `StudyDate`, `StudyTime`, `ModalitiesInStudy`, `StudyDescription`, `source nodes`, `cache status`
  - Conexión SSE y render incremental.

**Dependencies**
- Milestone 2 (config + DB listos).
- **Bloqueo parcial por inputs humanos**: hostnames/URLs reales del/los PACS remotos + Keycloak details para validación end-to-end con remoto.  
  - Sin esos datos, se testea con “mock node” o un Orthanc/dcm4chee de laboratorio.

**Exit criteria (testable)**
- `POST /api/search` devuelve `search_id` y `events_url`.
- UI muestra resultados parciales en menos de N segundos (configurable) y deduplica por `StudyInstanceUID`.
- En Postgres: se crean `search_requests` + `search_node_runs` con latencias/estado por nodo.
- Logs/audit minimizan PHI (no guardar `patient_name` en `integration_audit`; `query_json` en `search_requests` puede incluirlo si se considera “operativo”, pero audit/logs deben redactarlo).

---

## Milestone 4 — Retrieve jobs (C-MOVE preferido) + completitud por polling Orthanc
**Goal**: Permitir “Retrieve manual” persistido, ejecutado por worker, con transición de estados y criterio de completitud estable.

**Deliverables**
- API:
  - `POST /api/retrieve` (study_instance_uid, source_node_id) → `job_id`
  - `GET /api/retrieve/{job_id}` → estado + timestamps + error
- Scheduler/worker:
  - cola persistida en `retrieve_jobs` (`queued→running→done/failed`)
  - ejecución de retrieve por nodo:
    - **C-MOVE**: usar tooling DIMSE (dcmtk) dentro del contenedor backend
    - destino: Orthanc local (AE/host/port configurables)
  - completitud:
    - polling Orthanc por `StudyInstanceUID`
    - stable window + timeout global (por config)
    - guarda `orthanc_study_id`, `instances_received`
- UI:
  - Botón “Retrieve” por resultado (si `cache.present=false`)
  - Estado del job (poll simple al backend)
  - “Visualizar” deshabilitado hasta `job=done` (decisión humana ya confirmada)
  - Apertura de OHIF sólo sobre estudios puntuales seleccionados desde el portal

**Dependencies**
- Milestone 3 (búsqueda y UI listos; se requiere `StudyInstanceUID` real).
- Conectividad DIMSE a remoto (firewall/VPN) y reachability del Orthanc DICOM port desde el PACS remoto para C-MOVE.

**Exit criteria (testable)**
- Crear job y ver transición `queued→running→done`.
- Orthanc muestra el estudio en caché y OHIF lo abre desde `/dicomweb`.
- Orthanc muestra el estudio en caché y OHIF lo abre desde `/dicom-web`.
- Timeout produce `failed_timeout` y queda auditado.

---

## Milestone 5 — Retrieve fallback (C-GET via Orthanc) + endpoint cache status
**Goal**: Soportar nodos donde C-MOVE no es posible, usando C-GET “orquestado” por Orthanc, y exponer estado de caché por UID.

**Deliverables**
- Config por nodo: `retrieve_method = move|get|auto`.
- Implementación C-GET:
  - backend llama Orthanc REST para iniciar retrieve hacia remoto (Orthanc como SCU).
  - reutiliza mismo polling de completitud.
- `GET /api/cache/studies/{study_instance_uid}`:
  - `present`, `orthanc_study_id` y timestamps (si hay index)
- Robustez:
  - manejo de reintentos (mínimo: 1 retry configurable)
  - parsing de errores dcmtk/Orthanc y persistencia en `retrieve_jobs.error`

**Dependencies**
- Milestone 4 (jobs + polling ya implementados).

**Blocked until open decision is resolved**
- **Sí (bloqueante)**: “Which exact Orthanc REST endpoints and payloads will be used for C-GET orchestration”.  
  Sin esto, no se puede implementar C-GET de manera interoperable y testeable.

**Exit criteria (testable)**
- Con un nodo configurado como `get`, `POST /api/retrieve` completa con `done` y el estudio se visualiza en OHIF.
- `GET /api/cache/studies/{uid}` refleja presencia real (source of truth = Orthanc).

---

## Milestone 6 — Patient list surface + physician async panel
**Goal**: Separar formalmente las superficies de paciente y profesional para que OHIF no sea la lista principal de estudios.

**Deliverables**
- Patient surface:
  - endpoint backend para `patient studies`
  - lista propia del portal con estudios autorizados
  - apertura puntual en OHIF
- Physician surface:
  - endpoint/backend para búsqueda federada
  - lista con nodos remotos, disponibilidad local, estado de retrieve y acciones
  - retrieve asincrónico visible en UI
- Configuración de OHIF:
  - study list nativa deshabilitada para los flujos finales de paciente y médico

**Dependencies**
- Milestones 3–5.
- Definición posterior del modelo de auth real para paciente y profesional.
- Publicar contratos explícitos de UI y handoff a viewer para paciente y profesional.

**Exit criteria (testable)**
- El paciente navega solo estudios del portal y no ve la study list nativa de OHIF.
- El profesional opera desde un panel propio del portal y abre OHIF solo sobre estudios seleccionados.
- La protección futura del viewer queda pendiente para backend/proxy por sesión activa + `StudyInstanceUID`.

---

## Milestone 7 — Retención: purga Orthanc 7 días + limpieza Postgres 30 días (cron backend)
**Goal**: Evitar crecimiento ilimitado de caché y tablas operativas, con tareas auditables e idempotentes.

**Deliverables**
- Backend cron (ticker interno) con dos tareas:
  1. **Purge Orthanc**: identifica estudios expirados (7 días) y los borra vía Orthanc REST; actualiza `cached_studies`.
  2. **Purge DB**: elimina/archiva `integration_audit`, `search_requests`, `search_node_runs` > 30 días (configurable).
- Auditoría técnica por tarea: `integration_audit` con conteos (borrados, errores).
- Guards:
  - no borrar estudios con retrieve `running`
  - manejo de errores parcial (continue-on-error)

**Dependencies**
- Milestone 2 (DB) + Milestone 4 (cached_studies/retrieve_jobs).

**Exit criteria (testable)**
- Forzar un estudio “expirado” (manipulando timestamps en `cached_studies`) y verificar que se elimina de Orthanc y del índice.
- Forzar registros antiguos en `integration_audit` y confirmar limpieza automática.
- Logs sin PHI adicional (solo UIDs/timestamps/ids técnicos).

---

## Milestone 8 — End-to-end hardening MVP (proxy allowlist + token handling + test suite)
**Goal**: Cerrar el MVP con controles mínimos y pruebas repetibles.

**Deliverables**
- Nginx:
  - allowlist precisa de paths DICOMweb requeridos por OHIF
  - límites básicos (body size/timeouts) y headers de proxy
- Backend:
  - cache de token Keycloak con TTL; refresh on 401
  - nunca loggear token ni `client_secret`
- Tests:
  - Unit tests: dedup/merge policy, “partial filtering” flags
  - Integration tests (docker compose): health, search SSE basic contract, retrieve job state machine (con PACS de laboratorio si existe)
- Documentación “Runbook”:
  - cómo levantar stack
  - cómo agregar nodo PACS en `config.json`
  - cómo probar search/retrieve/view
  - troubleshooting (logs, tablas clave)

**Dependencies**
- Milestones 1–6.

**Exit criteria (testable)**
- Script de pruebas (make target o bash) que:
  - levanta compose
  - valida health
  - ejecuta una búsqueda (aunque sea contra nodo mock/lab)
  - valida contrato SSE (recibe `done`)
- Revisión manual: Orthanc admin endpoints no expuestos por Nginx.

---

## Blockers Summary (Milestones blocked by open human inputs/decisions)
- **Milestone 3 (parcialmente bloqueado)**: requiere URLs/hostnames de PACS remotos y Keycloak details para validar QIDO-RS real; se puede avanzar con nodo mock/lab.
- **Milestone 4 (dependiente del entorno)**: requiere conectividad DIMSE real (C-MOVE) desde PACS remoto hacia Orthanc local.
- **Milestone 5 (bloqueado)**: requiere decisión/confirmación de **endpoints Orthanc REST para orquestar C-GET** (payloads y comportamiento esperado).
- **Milestone 6 (parcialmente bloqueado)**: requiere definición funcional exacta de la lista del paciente y del panel del médico cuando se conecten los flujos reales de autenticación.

---

# First Build Slice
El slice más pequeño “end-to-end” para demostrar valor (search → retrieve → view) con el mínimo de piezas:

1. **Compose + Nginx boundary + OHIF** (Milestone 1, mínimo viable).
2. **Backend + Postgres con migraciones + config loader** (parte esencial de Milestone 2).
3. **Landing pública + búsqueda con SSE contra 1 nodo remoto QIDO-RS (o nodo lab) + LocalCacheHandler** (subset de Milestone 3).
4. **Retrieve C-MOVE único + polling completitud + link Visualizar a OHIF** (subset de Milestone 4).
5. **UI mínima**: landing + formulario + tabla + botón Retrieve + botón Visualizar (incluido en Milestone 3/4).

**Definición de “Done” del First Build Slice**
- Un operador puede:
  - abrir `http://localhost:8080/`
  - ver la landing pública con branding y flujos paciente/profesional
  - buscar estudios (SSE) y ver al menos 1 resultado deduplicado
  - disparar retrieve (C-MOVE) y esperar a `done`
  - abrir OHIF y visualizar el estudio desde `/dicom-web` (Orthanc local) sin acceso directo a PACS remoto.
