# Especificación Técnica (MVP): Portal DICOM Agregador + Caché Local + OHIF

## 0) Estado, decisiones confirmadas y supuestos

### Decisiones confirmadas (Human Decisions Log)
- El MVP se entrega **antes** de cualquier feature de autenticación (médicos/pacientes/OTP/sesiones/JWT/share links: **fuera**).
- Primer entregable: **stack completo en Docker Compose**.
- Servicios mínimos: `orthanc` (caché), `backend` (Go), `postgres`, `nginx`, `ohif`.
- **Orthanc local obligatorio** como caché y **destino de retrieve** (Move SCP).
- **PostgreSQL** como persistencia operativa por defecto.
- Integración HIS **solo configurable** (valores provistos posteriormente).
- Configuración de PACS remotos (dcm4chee, Orthanc, legacy) **externalizada**.
- La landing pública forma parte del MVP como experiencia visual, aunque sin autenticación real.
- La marca pública del portal es **RedImagenesNQN**.
- La identidad visual de la landing toma como referencia la app **ANDES**.
- El flujo visible de paciente en la landing usa `Documento + OTP` como experiencia UI.
- El flujo visible de profesional en la landing usa `DNI / usuario + contraseña` como experiencia UI.
- La landing y las superficies propias del portal deben ser **responsive** para dispositivos móviles.
- La integración futura objetivo para profesionales es **LDAP provincial + MFA**.
- OHIF está fijado a `ohif/app:v3.11.1`.
- El visor consume el caché local por la ruta `/dicom-web/`.
- OHIF debe tratarse como **visor** y no como superficie primaria de búsqueda o control de acceso.
- El paciente debe navegar una lista propia del portal, no la study list nativa de OHIF.
- El médico debe trabajar sobre un panel propio del portal con búsqueda federada y retrieve asíncrono.

### Supuestos del MVP
- El acceso al stack en desarrollo será por red controlada (p. ej. LAN/VPN); no se expone Internet “abierto” sin hardening adicional.
- OHIF se configura para consumir **solo** DICOMweb del **Orthanc local**, nunca PACS remotos.

---

## 1) Objetivo del MVP
Proveer un portal operativo mínimo capaz de:
1. **Consultar** estudios en múltiples PACS remotos (concurrencia en Go workers).
2. **Deduplicar** resultados por `StudyInstanceUID`.
3. Permitir **disparar retrieve** desde un PACS remoto hacia el **Orthanc local**.
4. Visualizar en **OHIF** únicamente desde el **caché Orthanc**.
5. Registrar auditoría técnica (consultas, jobs, errores) y guardar configuración en Postgres.

---

## 2) Contexto del sistema (System Context)

### Componentes
- **UI Portal Pública (MVP)**: landing estática servida por Nginx para:
  - Mostrar branding institucional.
  - Presentar selector `Paciente` / `Profesional`.
  - Exponer el flujo visual de `Documento + OTP` para pacientes.
  - Exponer el flujo visual de `DNI / usuario + contraseña` para profesionales.
  - Enlazar a OHIF y a verificaciones operativas.
- **UI Operativa (MVP)**: página simple servida por Nginx o frontend mínimo para:
  - Buscar (invoca backend).
  - Ver resultados parciales (SSE o WebSocket).
  - Botón *Retrieve* y *Visualizar*.
- **UI Futura Paciente**:
  - lista propia del portal con estudios autorizados;
  - filtros simples y estado de disponibilidad;
  - acción de apertura puntual en OHIF.
- **UI Futura Profesional**:
  - búsqueda federada en PACS remotos;
  - resultados enriquecidos con contexto operativo;
  - retrieve bajo demanda;
  - apertura puntual en OHIF.
- **Backend Go (Aggregator/Coordinator)**:
  - Conectores a PACS remotos (QIDO-RS / C-FIND).
  - Orquestación de retrieve (C-MOVE/C-GET).
  - API interna para UI + endpoints de health/config.
- **Orthanc (Cache PACS local)**:
  - DICOM SCP para recibir objetos (C-STORE).
  - DICOMweb (WADO-RS/QIDO-RS) para OHIF.
  - Política de retención 7 días (y límite de disco).
- **OHIF Viewer**:
  - Configurado con DICOMweb endpoint apuntando a Orthanc local (vía Nginx).
- **PostgreSQL**:
  - Configuración de nodos, jobs, auditoría y estado de caché.
- **Nginx**:
  - Reverse proxy único (frontera pública del stack).
  - Sirve landing pública, UI operativa y OHIF.
  - Proxy a backend y a Orthanc DICOMweb.
  - Debe separar assets propios del portal de los assets raíz del contenedor OHIF.

### Sistemas externos
- **PACS remotos**: dcm4chee, Orthanc remoto, legacy DICOM (sin REST).
- **HIS (futuro)**: en MVP se guarda configuración, no se ejecutan consultas reales obligatorias.

---

## 3) Actores (MVP)
- **Operador técnico / integrador** (sin login): configura nodos y prueba búsqueda/retrieve.
- **Paciente** (sin auth real en MVP): visualiza el flujo de acceso basado en documento y OTP.
- **Profesional** (sin auth real en MVP): visualiza el flujo de acceso basado en DNI/usuario y contraseña.
- **Servicios remotos PACS**: responden QIDO-RS/C-FIND y envían estudios al Orthanc local vía C-STORE tras un C-MOVE/C-GET.
- **OHIF**: consume DICOMweb desde Orthanc local.

## 3.1 Principio de separación de superficies
- El **portal** decide qué estudios listar y qué acciones exponer por actor.
- **OHIF** solo visualiza estudios puntuales ya autorizados o seleccionados.
- La study list nativa de OHIF no constituye control de acceso ni debe usarse como frontera funcional para pacientes.

---

## 4) Arquitectura lógica (alto nivel)

### 4.1 Backend: módulos
- `api`:
  - Endpoints HTTP REST para búsqueda, retrieve, estado de jobs, health.
  - Endpoint SSE/WS para streaming de resultados.
- `dicom/handlers` (interfaz `DICOMHandler`):
  - `QIDORSHandler` (dcm4chee-arc, Orthanc remoto).
  - `CFINDHandler` (legacy, vía dcmtk u otra librería/CLI).
  - `LocalCacheHandler` (consulta disponibilidad en Orthanc local).
- `scheduler/worker`:
  - Pool de workers por consulta; timeouts por nodo.
  - Cola de jobs de retrieve (persistida en Postgres).
- `storage`:
  - Repositorios para Postgres (config, jobs, auditoría).
- `orthanc`:
  - Cliente REST a Orthanc para verificar presencia local, obtener Study/Series/Instances, mapear IDs.

### 4.2 Protocolos
- **Búsqueda remota**:
  - Preferente: **QIDO-RS** (HTTP).
  - Fallback: **C-FIND** (DICOM DIMSE).
- **Retrieve**:
  - Preferente: **C-MOVE** (remoto → Orthanc local como Move SCP).
  - Alternativa: **C-GET** (si C-MOVE no permitido en algunos sitios; implica C-STORE de retorno).
- **Visualización**:
  - **DICOMweb WADO-RS/QIDO-RS**: OHIF → Orthanc local.

---

## 5) Flujo de datos (Data Flow)

### 5.1 Búsqueda agregada (streaming)
1. UI llama `POST /api/search` con filtros (fecha, modalidad, nombre, ID).
2. Backend crea `search_request_id` y dispara consultas concurrentes:
   - A cada PACS remoto vía handler correspondiente.
   - En paralelo, consulta Orthanc local (para marcar “cache hit”).
3. Backend deduplica por `StudyInstanceUID`, agregando `locations[]` (nodos donde existe).
4. Backend transmite resultados parciales por:
   - **SSE**: `GET /api/search/{id}/events` (recomendado por simplicidad), o
   - WebSocket (si se requiere bidireccional).
5. UI renderiza incrementalmente.

**Decisión explícita (MVP):** usar **SSE** salvo que haya requisito concreto de WS (SSE es más simple detrás de Nginx).

### 5.2 Retrieve manual
1. UI llama `POST /api/retrieve` con:
   - `study_instance_uid`
   - `source_node_id` (nodo elegido/prioritario)
2. Backend crea un `retrieve_job` en Postgres (`queued`).
3. Worker ejecuta:
   - Si nodo soporta DICOMweb retrieve directo (no estándar), ignorar en MVP.
   - Ejecutar **C-MOVE** (o C-GET) desde PACS remoto hacia Orthanc.
4. Backend monitorea estado:
   - Por eventos/logs del comando (dcmtk) **y/o**
   - Polling de Orthanc: verificar que el estudio aparezca completo (ver §8).
5. Al completar: `retrieve_job=done`, el UI habilita “Visualizar”.

### 5.3 Visualizar (OHIF)
1. UI abre URL de OHIF con `StudyInstanceUID` o con route de OHIF configurada.
2. OHIF consulta QIDO/WADO contra `nginx -> orthanc`.
3. Orthanc sirve instancias desde caché local.

### 5.3.1 Flujo futuro de paciente
1. El portal valida identidad del paciente.
2. El backend compone una lista propia de estudios autorizados para ese paciente.
3. El paciente selecciona un estudio en el portal.
4. El portal abre OHIF directamente sobre ese estudio.
5. El paciente no navega la study list nativa de OHIF.

### 5.3.2 Flujo futuro de profesional
1. El profesional ingresa al portal mediante autenticación institucional.
2. El profesional usa un panel propio de búsqueda federada.
3. El portal muestra resultados con:
   - nodos PACS remotos;
   - disponibilidad local;
   - estado de retrieve;
   - acciones operativas.
4. El profesional dispara retrieve bajo demanda cuando corresponda.
5. El portal abre OHIF sobre el estudio puntual seleccionado.

### 5.4 Landing pública y acceso futuro
1. El usuario accede a `/` y visualiza la landing institucional.
2. Selecciona `Paciente` o `Profesional`.
3. En MVP, el flujo es sólo visual y no genera sesión.
4. En una fase posterior:
   - `Paciente`: validación `DNI + OTP`.
   - `Profesional`: validación `LDAP provincial + MFA`.

---

## 6) Límites de seguridad (Security Boundaries)

### 6.1 Frontera pública
- **Solo Nginx** expone puertos al exterior del compose.
- Backend, Postgres y Orthanc quedan en red docker interna (sin publicar puertos, salvo necesidad de DICOM).
- Los assets del portal público deben servirse desde una ruta propia para no colisionar con los assets raíz de OHIF.
- El acceso a estudios para pacientes o médicos no debe depender solo de ocultar o mostrar la study list nativa del visor.

### 6.2 Puertos (propuesta)
- Nginx: `80` (dev) y opcional `443` (si se agrega TLS).
- Orthanc DICOM (C-STORE/Move SCP): `4242` **publicado** hacia red donde están PACS remotos (si aplica).
- Orthanc HTTP: **no publicado** directamente; solo accesible vía Nginx.
- Postgres: no publicado.
- En desarrollo local, los puertos directos de Orthanc pueden quedar ligados a `127.0.0.1` por defecto.

### 6.3 Secretos y configuración
- Variables de entorno / archivos montados (`.env`, `config.json`) fuera del código.
- No almacenar secretos en imágenes Docker ni en repositorio.

### 6.4 Auditoría técnica (MVP)
- Log estructurado (JSON) en backend.
- Persistencia mínima en Postgres:
  - requests, errores de integración, latencias, estado de jobs.

---

## 7) Ciclo de vida del caché (Orthanc) y retención

### 7.1 Política
- Retención: **7 días**.
- Límite de disco: `max_disk_usage_gb` (configurable).
- Purga automática:
  - Preferible: plugin/setting de Orthanc + tarea periódica (si Orthanc no lo resuelve nativamente con esa lógica exacta).
  - Alternativa: job del backend (cron interno) que llama API Orthanc para borrar estudios expirados.

**Decisión MVP:** implementar purga vía **backend cron** (control explícito y auditable) si no hay garantía simple en Orthanc.

### 7.2 Estado “en caché”
- La verdad de “existe localmente” es Orthanc.
- Postgres mantiene un **índice operativo**:
  - `cached_studies` con timestamps y referencias para acelerar UI y jobs.

---

## 8) Detección de completitud de retrieve (MVP)
Problema: Orthanc puede recibir instancias progresivamente.

### Estrategia MVP (determinística y simple)
- Tras iniciar retrieve:
  - Poll cada N segundos el Orthanc REST:
    - Buscar Study por `StudyInstanceUID` (si ya existe).
    - Consultar `Instances` count actual.
  - Considerar “completo” cuando:
    - La cuenta de instancias no cambia durante `stable_window` (p. ej. 30–60s), **o**
    - Se alcanza un timeout máximo (configurable, p. ej. 20 min) ⇒ `failed_timeout`.
- Guardar métricas: duración, instancias recibidas.

**Nota:** en fases futuras puede integrarse con logs DICOM del remoto o eventos de Orthanc.

---

## 9) Especificación de API (Backend Go)

### 9.1 Endpoints (MVP)
#### Health
- `GET /api/health`
  - 200 + estado (db ok, orthanc ok).

#### Config (operador técnico)
- `GET /api/config`
- `PUT /api/config`
  - Carga/actualiza nodos PACS + cache config + his config (solo persistir).

> MVP puede iniciar con configuración por archivo JSON montado, y exponer `GET` para verificar. `PUT` opcional si se desea UI de administración mínima.

#### Search
- `POST /api/search`
  - Body:
    ```json
    {
      "patient_name": "DOE^JOHN",
      "patient_id": "123",
      "dni": "20123456",
      "date_from": "2026-01-01",
      "date_to": "2026-01-31",
      "modalities": ["CT","MR"]
    }
    ```
  - Response:
    ```json
    { "search_id": "uuid", "events_url": "/api/search/uuid/events" }
    ```

- `GET /api/search/{search_id}/events` (SSE)
  - Eventos:
    - `node_started`, `node_result`, `node_finished`, `error`, `done`
    - `study` (resultado deduplicado incremental)

#### Retrieve
- `POST /api/retrieve`
  - Body:
    ```json
    { "study_instance_uid": "1.2.3...", "source_node_id": "sede_central" }
    ```
  - Response:
    ```json
    { "job_id": "uuid", "status": "queued" }
    ```

- `GET /api/retrieve/{job_id}`
  - Estado: `queued|running|done|failed`
  - Campos: timestamps, node, mensaje error.

#### Cache status
- `GET /api/cache/studies/{study_instance_uid}`
  - Indica si está en Orthanc y, si aplica, Orthanc Study ID.

### 9.2 Criterios de deduplicación
- Clave: `StudyInstanceUID`.
- Merge:
  - `locations[]`: `{node_id, protocol, last_seen, priority, latency_ms_estimate}`
  - `cache`: `{present: bool, orthanc_id?: string}`

---

## 10) Modelo de datos (PostgreSQL)

### 10.1 Tablas mínimas (propuesta)
- `pacs_nodes`
  - `id` (pk, text)
  - `name`
  - `protocol` (`qido_rs|c_find`)
  - `priority` (int)
  - `timeout_ms` (int)
  - **QIDO-RS fields**: `dicomweb_base_url`, `auth_type`, `auth_secret_ref`
  - **DIMSE fields**: `ae_title`, `host`, `port`, `calling_ae_title` (si aplica)
  - `enabled` (bool)
  - `created_at`, `updated_at`

- `his_config` (MVP: solo persistencia)
  - `base_url`, `auth_type`, `api_key_ref`, `params_json`

- `search_requests`
  - `id` (uuid)
  - `query_json`
  - `created_at`
  - `status` (`running|done|failed`)

- `search_node_runs`
  - `id` (uuid)
  - `search_request_id` (fk)
  - `node_id` (fk)
  - `started_at`, `finished_at`
  - `status` (`running|done|failed|timeout`)
  - `error`
  - `latency_ms`

- `retrieve_jobs`
  - `id` (uuid)
  - `study_instance_uid`
  - `source_node_id`
  - `status` (`queued|running|done|failed`)
  - `error`
  - `created_at`, `started_at`, `finished_at`
  - `orthanc_study_id` (nullable)
  - `instances_received` (int, nullable)

- `cached_studies`
  - `study_instance_uid` (pk)
  - `orthanc_study_id`
  - `first_seen_at`
  - `last_verified_at`
  - `expires_at`

- `integration_audit`
  - `id` (uuid)
  - `type` (`search|retrieve|orthanc|his|proxy`)
  - `ref_id` (search_id/job_id/etc)
  - `message`
  - `data_json`
  - `created_at`

### 10.2 Migraciones
- Usar migraciones versionadas (golang-migrate o similar).
- Acceptance: `docker compose up` crea esquema automáticamente (init + migrate).

---

## 11) Docker Compose (MVP)

### 11.1 Servicios
- `postgres`
  - Volumen persistente.
  - Variables: `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`.
- `backend`
  - Build local (Dockerfile Go).
  - Env: `DATABASE_URL`, `CONFIG_PATH`, `ORTHANC_BASE_URL`.
  - Red interna.
- `orthanc`
  - Volúmenes: `orthanc-db`, `orthanc-storage`.
  - Config montada (JSON).
  - Publicar puerto DICOM `4242` (según entorno).
- `ohif`
  - Build o imagen oficial OHIF.
  - Config para DICOMweb en `/dicomweb` proxied a Orthanc.
- `nginx`
  - Publica `80`.
  - Config:
    - `/api` → backend
    - `/ohif` → ohif
    - `/dicomweb` → orthanc HTTP (limitado a DICOMweb paths)
    - `/` → UI estática (opcional)

### 11.2 Redes
- `frontend_net`: nginx + ohif
- `backend_net`: nginx + backend + orthanc + postgres

### 11.3 Constraints
- El compose debe levantar “one command” en dev.
- Logs accesibles por `docker compose logs`.

---

## 12) Configuración externa (JSON + env)

### 12.1 Archivo `config.json` (alineado con tu ejemplo)
- `pacs_nodes[]`: incluye campos por protocolo (QIDO-RS vs DIMSE).
- `cache_config`: retención, límite disco, parámetros Orthanc.
- `his_config`: persistir valores (no ejecutar lógica obligatoria).

### 12.2 Referencias de secretos
- En JSON guardar `*_secret_ref` (nombre de env var o path de archivo), no el secreto en claro.
- Backend resuelve el secreto desde env o archivo montado.

---

## 13) Criterios de aceptación (Acceptance Criteria)

### Infraestructura
- `docker compose up` levanta `orthanc`, `backend`, `postgres`, `nginx`, `ohif` sin pasos manuales adicionales.
- Nginx es el único punto de entrada HTTP (backend/orthanc no expuestos directamente por HTTP).

### Búsqueda
- Ejecutar una búsqueda dispara consultas concurrentes a **≥2** nodos configurados (cuando existan).
- La UI recibe resultados parciales vía SSE y muestra estudios deduplicados por `StudyInstanceUID`.
- Se registra en Postgres al menos: `search_requests`, `search_node_runs`, auditoría técnica.

### Retrieve
- Botón/endpoint `retrieve` crea job persistente.
- El job transiciona `queued → running → done/failed`.
- Al completar, el estudio queda disponible en Orthanc y OHIF puede abrirlo desde el caché local.

### Visualización
- OHIF consume exclusivamente `/dicomweb` (Orthanc local).
- No existe configuración en OHIF que apunte directamente a PACS remotos.

### Retención
- Existe mecanismo automático (cron backend o equivalente) que elimina estudios expirados (>7 días) y actualiza `cached_studies`.

---

## 14) Decisiones explícitas de implementación (MVP)
- Streaming de resultados: **SSE**.
- Persistencia operativa: **PostgreSQL**.
- Caché de imágenes y DICOMweb: **Orthanc local**.
- Reverse proxy: **Nginx** (única exposición pública HTTP).
- Retrieve: **C-MOVE preferido**, con opción de **C-GET** si un nodo no permite C-MOVE (configurable por nodo).
- Purga: **backend cron** si Orthanc no garantiza la política requerida de forma simple.

---

## Open Questions Requiring Human Decision
1. **Puertos/hostnames públicos**: ¿qué hostname/puerto debe exponer Nginx en el entorno objetivo (dev/staging/on-prem)? ¿Se requiere TLS en MVP?
2. **DIMSE networking**: ¿el Orthanc local (Move SCP/C-STORE) será alcanzable desde todos los PACS remotos? (firewall/NAT/VPN). En caso contrario, ¿se permite C-GET desde el backend?
3. **Soporte Legacy**: para PACS sin DICOMweb, ¿se confirma uso de **dcmtk** dentro del contenedor backend (licenciamiento/instalación) o hay librería preferida?
4. **UI del MVP**: ¿se requiere UI web mínima (lista + botones) o basta con API + ejemplos (curl/Postman) para el primer hito?
5. **Estrategia de completitud**: ¿aceptan “stable window polling” como criterio de completitud de retrieve, o requieren una señal más fuerte (p.ej. conteo esperado de instancias desde metadata remota)?
6. **Metadatos en Postgres**: ¿hasta qué nivel se persiste metadata (solo studies vs series/instances) para acelerar UX y reporting técnico?
