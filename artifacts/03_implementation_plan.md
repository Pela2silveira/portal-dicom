# Plan de Implementación (MVP) — Portal DICOM Agregador + Caché (Orthanc) + OHIF

> Este plan se mantiene como **plan vivo**. Los primeros milestones ya están sustancialmente implementados; los ítems “Done/Current/Next” indican el estado real del repo.

## Milestone 1 — Compose stack + reverse proxy boundary
**Goal**: Levantar el “one command up” con la frontera HTTP única en Nginx, branding base y servicios internos cableados.

**Status**: **Done (baseline establecida)**

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
- Orthanc configurado (DICOM + DICOMweb) accesible **solo** vía Nginx HTTP; DICOM port `4242` publicado según compose.
- OHIF configurado y pinneado a `ohif/app:v3.11.1`, consumiendo DICOMweb en `/dicom-web` (a través de Nginx).
- Landing pública con branding `RedImagenesNQN`, assets propios y referencia visual ANDES.
- Landing pública responsive para dispositivos móviles.

**Exit criteria (testable)**
- `docker compose up` levanta todo sin pasos manuales.
- `GET http://localhost:8080/api/health` ⇒ 200 y reporta `db_ok` + `orthanc_ok`.
- `GET http://localhost:8080/` carga la landing pública con logo y favicon propios.
- `GET http://localhost:8080/ohif/` carga UI de OHIF.
- Verificación negativa: URLs de Orthanc admin REST (p.ej. `/instances`, `/studies` si no son DICOMweb) **no** son accesibles vía Nginx (403/404), mientras que DICOMweb sí (p.ej. `/dicom-web/studies` responde según Orthanc).

---

## Milestone 2 — Persistencia + migraciones + configuración externalizada
**Goal**: Tener Postgres operativo con migraciones, modelo mínimo, y carga/validación de `config.json` + secretos por env/file refs.

**Status**: **Done (baseline establecida)**

**Deliverables**
- Migraciones versionadas:
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
  - validación de schema + “secret_ref/env resolution” desde env o archivo montado
- Endpoint `GET /api/config` (read-only en MVP inicial; `PUT` opcional posterior).

**Exit criteria (testable)**
- Arranque del backend ejecuta migraciones automáticamente.
- Cambiar `config.json` (p.ej. nombre/priority de nodo) se refleja en `GET /api/config`.
- Logs y `integration_audit` no incluyen `client_secret` ni tokens (verificación manual en logs + DB).
- Contraseñas de médicos y códigos por mail no se persisten en claro en ninguna tabla.

---

## Milestone 3 — Landing pública + superficies del portal
**Goal**: Consolidar la landing pública y las superficies propias de paciente/profesional para que el portal sea el control plane y OHIF quede solo como visor.

**Status**: **Done (baseline establecida)**

**Deliverables**
- Landing pública:
  - selector `Paciente` / `Profesional`
  - flujo visual `Documento + código por mail`
  - flujo visual `DNI + contraseña`
  - textos alineados con roadmap `LDAP provincial + MFA` para médicos
  - aclaración visual de que el portal es la superficie de acceso y OHIF es el visor
  - adaptación responsive para teléfonos y tablets
- Superficie paciente:
  - input `Documento`
  - botón “Actualizar lista”
  - lista de estudios
  - botón `Retrieve` por estudio pending
  - botón `Visualizar` cuando el estudio esté local
  - flag `patient.fake_auth` para alternar rápido entre demo auth y auth real en construcción
- Superficie profesional:
  - grilla de resultados
  - acciones Retrieve / Visualizar
  - carga inicial desde Orthanc local con estudios en cache de la semana actual

**Exit criteria (testable)**
- Navegación: `/` → superficie paciente/profesional sin pasar por OHIF.
- Responsive básico (viewport mobile) sin overflow crítico.
- Acciones de visualización abren OHIF en nueva pestaña.

---

## Milestone 4 — Búsqueda agregada con SSE + deduplicación
**Goal**: Implementar `POST /api/search` con fan-out concurrente a múltiples nodos, streaming SSE incremental, deduplicación por `StudyInstanceUID` y persistencia operacional.

**Status**: **Pending**

**Deliverables**
- API:
  - `POST /api/search` → `{search_id, events_url}`
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
- UI Operativa/Profesional:
  - Conexión SSE y render incremental real.

**Dependencies**
- Config + DB listos.
- Al menos 2 nodos operativos o nodos simulados para validar multi-node.

**Exit criteria (testable)**
- `POST /api/search` devuelve `search_id` y `events_url`.
- UI muestra resultados parciales en menos de N segundos (configurable) y deduplica por `StudyInstanceUID`.
- En Postgres: se crean `search_requests` + `search_node_runs` con latencias/estado por nodo.
- Logs/audit minimizan PHI.

**Blocked by open decisions?**
- **Parcialmente bloqueado**: requiere ≥2 nodos para validar el caso federado real.  
- **Aporte nuevo a incorporar**: evaluar incluir un **Orthanc remoto simulado** en compose para CI/dev y así destrabar validación repetible del flujo federado.

---

## Milestone 5 — Retrieve jobs + completitud + handoff a OHIF
**Goal**: Permitir “Retrieve manual” persistido, ejecutado por Orthanc contra nodos remotos, con transición de estados y criterio de completitud estable.

**Status**: **Done for first slice / needs hardening**

**Deliverables**
- API:
  - `POST /api/retrieve` + `GET /api/retrieve/{job_id}`
  - `GET /api/cache/studies/{study_instance_uid}`
  - Paciente: `GET /api/patient/studies?document=<dni>` + `POST /api/patient/retrieve`
  - Profesional: `GET /api/physician/results?...` + `POST /api/physician/retrieve`
- Orquestación C-GET:
  - Backend configura modalidad en Orthanc (`PUT /modalities/{id}`) si corresponde
  - Dispara retrieve `POST /modalities/{id}/get` con `StudyInstanceUID`
  - Timeout específico para retrieve (no usar timeout HTTP corto global)
- Polling completitud (MVP):
  - busca Study por `StudyInstanceUID`
  - observa `instances_count` y considera done con `stable_window` o `global_timeout`
- Persistencia:
  - `retrieve_jobs` transiciones `queued → running → done/failed`
  - `cached_studies` actualizado con `expires_at`, `last_verified_at`
- Handoff a OHIF:
  - Portal construye URL `GET /ohif/viewer?StudyInstanceUIDs=<uid>`
  - UI abre en pestaña nueva; “Visualizar” solo en `done`

**What is already working**
- Paciente:
  - `GET /api/patient/studies?document=<dni>` con QIDO real al nodo configurado
  - `POST /api/patient/retrieve` con `C-GET` vía Orthanc REST
  - actualización a `available_local`
- Profesional:
  - `POST /api/physician/retrieve`
  - recalculado de `cacheStatus`, `retrieveStatus` y `viewer_url`

**Current hardening still needed**
- Límites de concurrencia de retrieve por nodo/global
- Timeouts operativos explícitos por job
- Mejor diferenciación de estados `failed/running/retryable`
- Formalizar la preferencia `C-MOVE` vs `C-GET` por nodo

**Exit criteria (testable)**
- `POST /api/patient/retrieve` crea `retrieve_job` en DB.
- Job avanza a `done` al aparecer el estudio en Orthanc (según polling).
- `Visualizar` abre OHIF y OHIF carga el estudio desde `/dicom-web/` (no desde remoto).
- Comportamiento de timeout: si no completa a tiempo → `failed_timeout` (o `failed` con error).

**Blocked by open decisions?**
- **Sí, parcialmente** por conectividad DIMSE real si se quiere mover a `C-MOVE` por defecto en ciertos nodos.

---

## Milestone 6 — Patient list surface + physician async panel
**Goal**: Consolidar las superficies de paciente y profesional para que OHIF no sea la lista principal de estudios.

**Status**: **Current / partially done**

**Already implemented**
- Patient surface:
  - endpoint backend para `patient studies`
  - lista propia del portal con estudios autorizados
  - apertura puntual en OHIF
  - primer slice funcional con QIDO `PatientID=<dni>` contra el único nodo PACS configurado y sincronización en `patient_study_access`
  - primer retrieve funcional con botón `Retrieve`, `POST /api/patient/retrieve` y orquestación `C-GET` vía Orthanc REST
  - observabilidad estructurada del sync paciente: token, QIDO, duración y conteos
  - sin persistir métricas de observabilidad en Postgres; sólo logs y futuros stats en memoria
- Physician surface:
  - `GET /api/physician/results?username=<dni>` contra Orthanc local cuando no hay filtros
  - búsqueda remota real por QIDO cuando el profesional aplica filtros
  - `POST /api/physician/retrieve` con actualización de estado local contra DB/Orthanc

**Remaining work**
- Búsqueda federada real multi-nodo para profesional
- Normalización de filtros entre nodos
- Contrato definitivo de campos y estados en la grilla profesional
- Definir si existirá o no una superficie `/portal/operator` separada
- Resolver el riesgo de `PatientID == DNI` como contrato demasiado rígido para paciente

**Exit criteria (testable)**
- El paciente navega solo estudios del portal y no ve la study list nativa de OHIF.
- Los estudios `pending_retrieve` del paciente pueden recuperarse manualmente y pasan a `available_local` antes de abrir OHIF.
- El profesional opera desde un panel propio del portal y abre OHIF solo sobre estudios seleccionados.
- La protección futura del viewer queda pendiente para backend/proxy por sesión activa + `StudyInstanceUID`.

---

## Milestone 7 — Retención: purga Orthanc 7 días + limpieza Postgres 30 días (cron backend)
**Goal**: Evitar crecimiento ilimitado de caché y tablas operativas, con tareas auditables e idempotentes.

**Status**: **Pending**

**Deliverables**
- Backend cron (ticker interno) con dos tareas:
  1. **Purge Orthanc**: identifica estudios expirados (7 días) y los borra vía Orthanc REST; actualiza `cached_studies`.
  2. **Purge DB**: elimina/archiva `integration_audit`, `search_requests`, `search_node_runs` > 30 días (configurable).
- Auditoría técnica por tarea: `integration_audit` con conteos (borrados, errores).
- Guards:
  - no borrar estudios con retrieve `running`
  - manejo de errores parcial (continue-on-error)
- Hardening de rutas:
  - allowlist precisa para `/dicom-web/*`
  - headers/flags correctos para SSE (sin buffering)

**Dependencies**
- DB + retrieve jobs/cached_studies implementados.

**Exit criteria (testable)**
- Forzar un estudio “expirado” y verificar que se elimina de Orthanc y del índice.
- Forzar registros antiguos en `integration_audit` y confirmar limpieza automática.
- Logs sin PHI adicional (solo UIDs/timestamps/ids técnicos).

---

## Milestone 8 — End-to-end hardening MVP (proxy allowlist + token handling + test suite)
**Goal**: Cerrar el MVP con controles mínimos y pruebas repetibles.

**Status**: **Pending**

**Deliverables**
- Nginx:
  - allowlist precisa de paths DICOMweb requeridos por OHIF
  - límites básicos (body size/timeouts) y headers de proxy
- Backend:
  - cache de token Keycloak con TTL; refresh on 401
  - nunca loggear token ni `client_secret`
  - límites operativos:
    - `max_concurrent_retrieves_global`
    - `max_concurrent_retrieves_per_node`
    - timeout total por job
- Tests:
  - Unit tests: dedup/merge policy, “partial filtering” flags
  - Integration tests (docker compose): health, search SSE basic contract, retrieve job state machine
  - evaluar incluir “remote Orthanc simulated” como soporte de CI/dev
- Documentación “Runbook”:
  - cómo levantar stack
  - cómo agregar nodo PACS en `config.json`
  - cómo probar search/retrieve/view
  - troubleshooting (logs, tablas clave)

**Dependencies**
- Milestones 1–7.

**Exit criteria (testable)**
- Script de pruebas que:
  - levanta compose
  - valida health
  - ejecuta una búsqueda (aunque sea contra nodo mock/lab)
  - valida contrato SSE (recibe `done`)
- Revisión manual: Orthanc admin endpoints no expuestos por Nginx.

---

## Blockers Summary (Milestones blocked by open human inputs/decisions)
- **Milestone 4 (parcialmente bloqueado)**: requiere ≥2 nodos para validar búsqueda federada real; se puede destrabar con nodo mock/lab o Orthanc remoto simulado.
- **Milestone 5 (dependiente del entorno)**: requiere conectividad DIMSE real si se quiere validar `C-MOVE` en vez de `C-GET`.
- **Milestone 6 (parcialmente bloqueado)**: requiere definir estrategia de identificador paciente (`PatientID == DNI` vs configurable/HIS).
- **Desbloqueo táctico aceptado**: hasta disponer de la API REST del HIS, se permite un adapter backend-only `his_mongo_direct` para lectura de identidad de paciente, siempre que sea read-only, performante y reemplazable por el provider REST futuro.
- **Claves candidatas ya acordadas para PACS**: `documento` y string de `_id` provenientes del documento `paciente` de Mongo.
- **Estado actual del backend**: `PatientIdentitySource` ya soporta `his_mongo_direct`, usa la colección `paciente` y persiste resoluciones exitosas en Postgres.
- **Estado actual del acceso paciente**: `Enviar código` ya prevalida existencia de paciente y mail activo antes del futuro envío real del correo.
- **Estado actual de búsqueda paciente**: `POST /api/patient/search` encola búsquedas remotas en `search_requests`/`search_node_runs`, `GET /api/patient/search?request_id=...` reporta estado, y `GET /api/patient/studies` queda como lectura pura de resultados cacheados.
- **Compatibilidad de schema dev**: bases creadas con la migración inicial requieren una migración adicional para `patients.gender_identity`.
- **Milestone 8 (parcialmente bloqueado)**: requiere decidir controles mínimos para entornos no-localhost (`X-Portal-Key`, allowlist IP, o similar).

---

# Current Build Slice
El slice mínimo **ya implementado** que demuestra valor real hoy es:

1. **Compose + Nginx boundary + OHIF**.
2. **Backend + Postgres con migraciones + config loader**.
3. **Landing pública + superficies paciente/profesional**.
4. **Paciente**:
   - QIDO real al único nodo remoto configurado
   - retrieve real con `C-GET` vía Orthanc REST
   - handoff a OHIF por `StudyInstanceUID`
5. **Profesional**:
   - estudios locales de la semana actual sin filtros
   - QIDO real con filtros
   - retrieve real desde la grilla

**Definición de “Done” del slice actual**
- Un usuario puede:
  - abrir `http://localhost:8080/`
  - ver la landing pública con branding y flujos paciente/profesional
  - consultar estudios de paciente contra nodo remoto real
  - disparar retrieve y esperar a `done`
  - abrir OHIF y visualizar el estudio desde `/dicom-web` (Orthanc local) sin acceso directo a PACS remoto

---

# Next Recommended Slice
El próximo slice de mayor valor es:

1. **Búsqueda profesional federada real multi-nodo**
2. **Hardening de rutas `/dicom-web` y exposición fuera de localhost**
3. **Límites operativos de retrieve**
4. **Definir estrategia configurable del identificador paciente**
5. **Agregar entorno remoto simulado para CI/dev si sigue haciendo falta repetibilidad**
