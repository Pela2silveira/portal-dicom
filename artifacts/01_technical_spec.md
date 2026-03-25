# Especificación Técnica de Implementación (MVP) — Portal DICOM Agregador + Caché + Viewers

> **Objetivo del MVP:** entregar un portal web operativo mínimo que agregue búsquedas sobre PACS remotos, coordine **retrieve** hacia un **Orthanc local (caché)** y visualice **exclusivamente desde la caché** en **Stone Web Viewer** y **OHIF**, todo levantable con **Docker Compose** y expuesto por **Nginx** en `http://localhost:8081`.

## 0) Estado, decisiones confirmadas y supuestos

### Decisiones confirmadas (Human Decisions Log)
- El MVP se entrega **antes** de cualquier feature de autenticación (médicos/pacientes/código por mail/sesiones/JWT/share links: **fuera**).
- Primer entregable: **stack completo en Docker Compose**.
- Servicios mínimos: `orthanc` (caché), `backend` (Go), `postgres`, `nginx`, `ohif`.
- **Orthanc local obligatorio** como caché y **destino de retrieve** (Move SCP).
- **PostgreSQL** como persistencia operativa por defecto.
- Integración HIS **solo configurable** (valores provistos posteriormente).
- Excepción transitoria permitida: mientras no exista integración REST operativa del HIS, el backend puede consultar MongoDB en forma directa solo para resolver identidad de paciente e identificadores alternativos.
- Cuando el provider activo es `his_mongo_direct`, el backend consulta la colección Mongo `paciente` en modo read-only y persiste en Postgres los resultados exitosos ya normalizados.
- Los demográficos visibles del paciente (`full_name`, `birth_date`, `sex`, `gender_identity`) deben priorizar la fuente de identidad HIS/Mongo y no deben ser reemplazados por `PatientName` DICOM durante QIDO.
- Configuración de PACS remotos (dcm4chee, Orthanc, legacy) **externalizada**.
- La landing pública forma parte del MVP como experiencia visual, aunque sin autenticación real.
- La marca pública del portal es **RedImagenesNQN**.
- La identidad visual de la landing toma como referencia la app **ANDES**.
- El flujo visible de paciente en la landing usa `Documento + código por mail` como experiencia UI.
- El campo `Documento` del flujo paciente debe validarse tanto en frontend como en backend con formato numérico acotado antes de tocar búsquedas, retrieve o futuros pasos de verificación.
- Toda entrada editable del portal debe tener una regla explícita de normalización/saneamiento en frontend según su tipo (`numérico`, `texto libre acotado`, `selector`, `fecha`) y la misma semántica debe revalidarse en backend antes de usar el dato.
- El paso `Enviar código` debe consultar backend antes del envío real del mail para distinguir tres resultados: `ready_to_send`, `missing_active_email` y `patient_not_found`.
- Cuando `ready_to_send` y exista email registrado, el mensaje visible debe incluir el correo ofuscado, preservando sólo los primeros 3 caracteres antes de `@`.
- El modo de auth paciente debe poder alternarse por config (`patient.fake_auth`) para conmutar rápido entre demos y validación real por correo sin cambiar endpoints ni UI principal.
- Si `patient.fake_auth` no está presente en `config.json`, el backend debe asumir `true` para preservar compatibilidad con el MVP actual.
- El modo de auth profesional debe poder alternarse por config (`professional.fake_auth`) para desacoplar la validación transitoria actual del objetivo futuro `LDAP provincial + MFA`.
- Si `professional.fake_auth` no está presente en `config.json`, el backend debe asumir `true` para preservar compatibilidad con el MVP actual.
- La carga inicial del panel profesional sin filtros debe usar una ventana relativa configurable mediante `professional.initial_cache_period`.
- Si las dependencias del backend no están disponibles al arranque, el proceso debe permanecer vivo en modo degradado y publicar `/api/health` con `503` para habilitar el fallback de mantenimiento en Nginx.
- Docker Compose debe usar un endpoint separado de liveness (`/api/livez`) para la salud del contenedor y dejar `/api/health` como readiness operativa.
- Cuando `his.provider = his_mongo_direct` no pueda conectarse a Mongo al inicio, el backend debe reintentar la conexión cada 1 minuto sin reinicio.
- La salud operativa del provider Mongo debe evaluarse también después del arranque; si pierde conectividad luego de estar disponible, `/api/health` debe volver a `503`.
- `/api/health` debe publicar además el detalle de componentes `required` y `optional`, para distinguir indisponibilidad total de degradación parcial.
- El backend debe publicar `GET /api/system/events` como SSE de salud del sistema, emitiendo cambios de estado agregado y snapshot de componentes.
- El watcher agregado que recalcula salud de componentes debe correr cada 1 minuto; el SSE puede usar heartbeats separados para sostener la conexión.
- El health de PACS remotos debe soportar explícitamente `auth_qido` y `dimse_c_echo`; este último puede ejecutarse a través de Orthanc REST sobre `/modalities/{id}/echo`.
- Las fechas de estudio que llegan desde DICOM/QIDO deben normalizarse a `YYYY-MM-DD` antes de persistirse o filtrarse en superficies del portal.
- El flujo visible de profesional en la landing usa `DNI + contraseña` como experiencia UI.
- La landing y las superficies propias del portal deben ser **responsive** para dispositivos móviles.
- La integración futura objetivo para profesionales es **LDAP provincial + MFA**.
- OHIF está fijado a `ohif/app:v3.11.1`.
- El visor consume el caché local por la ruta `/dicom-web/`.
- OHIF debe tratarse como **visor** y no como superficie primaria de búsqueda o control de acceso.
- El paciente debe navegar una lista propia del portal, no la study list nativa de OHIF.
- La configuración actual de OHIF mantiene `showStudyList: false` y el root `/ohif/` redirige a la landing pública; el acceso soportado al visor es por URL puntual (`/ohif/viewer?...`).
- Cuando un estudio esté disponible localmente, el portal debe exponer además una descarga `ZIP DICOM` del estudio completo desde Orthanc, autorizada por backend.
- Las descargas `ZIP DICOM` de profesionales deben contabilizarse en Postgres y respetar un cupo semanal configurable (`professional.weekly_download_limit`), con rechazo explícito cuando se supere.
- El médico debe trabajar sobre un panel propio del portal con búsqueda federada y retrieve asíncrono.
- Los contratos explícitos de ambas superficies se documentan en `artifacts/05_ui_contracts.md`.
- Aun en el mock de la landing, ambos ingresos deben aterrizar primero en superficies del portal y no en la home general del visor.
- Las transferencias de estudios deben ocurrir entre **PACS remotos ↔ Orthanc local**; el backend solo coordina, persiste estado y observa el workflow.
- Orthanc es la **fuente de verdad** para decidir si un estudio está disponible localmente; Postgres mantiene un índice operacional reconstruible.
- Las métricas de observabilidad no se persisten por ahora en Postgres; se resuelven con logs estructurados y, si hace falta, stats en memoria.

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
  - Exponer el flujo visual de `Documento + código por mail` para pacientes.
  - Exponer el flujo visual de `DNI + contraseña` para profesionales.
  - Con `professional.fake_auth = true`, mantener la validación profesional transitoria vigente.
  - Con `professional.fake_auth = false`, reservar el acceso profesional para la futura autenticación institucional.
  - Validar el ingreso profesional contra la colección Mongo `profesional` cuando el provider operativo sea `his_mongo_direct`.
  - Permitir el acceso sólo si el documento existe, `habilitado == true`, `profesionalMatriculado == true`, y consta con matrícula profesional.
  - La matrícula profesional debe resolverse desde `formacionGrado[].matriculacion[]`, tomando la primera entrada con `baja.fecha == null` y usando `matriculaNumero` como valor visible.
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
  - Evaluación centralizada de componentes requeridos y opcionales para readiness operativa.
  - Stream SSE de cambios de salud del sistema para refresco automático de app vs mantenimiento.
  - Adapters por capacidad para nodos `dicomweb`, `dimse` y `hybrid`.
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
  - Debe servir una página estática de mantenimiento para `/` cuando el backend responda health degradado o no esté disponible.

### Sistemas externos
- **PACS remotos**: dcm4chee, Orthanc remoto, legacy DICOM (sin REST).
- **HIS (futuro)**: en MVP se guarda configuración, no se ejecutan consultas reales obligatorias.
- **HIS / MPI transitorio**:
  - el camino objetivo sigue siendo API REST;
  - mientras eso no esté disponible, se admite un adapter `his_mongo_direct` en backend;
  - ese adapter debe ser estrictamente read-only;
  - debe quedar encapsulado detrás de una abstracción de fuente de identidad de paciente;
  - no debe contaminar contratos externos ni modelado del portal con detalles de Mongo;
  - sus consultas deben ser performantes: uso de índices, filtros acotados, proyecciones mínimas y resultados limitados.
  - para búsquedas PACS del paciente, las claves candidatas provenientes de Mongo serán `documento` y el string de `_id`.
  - la implementación base del backend ya expone esa abstracción (`PatientIdentitySource`) y hoy la resuelve con un provider local de seed.

---

## 3) Actores (MVP)
- **Operador técnico / integrador** (sin login): configura nodos y prueba búsqueda/retrieve.
- **Paciente** (sin auth real en MVP): visualiza el flujo de acceso basado en documento y código por mail.
- **Profesional** (sin auth real en MVP): visualiza el flujo de acceso basado en DNI/usuario y contraseña.
- **Servicios remotos PACS**: responden QIDO-RS/C-FIND y envían estudios al Orthanc local vía C-STORE tras un C-MOVE/C-GET.
- **OHIF**: consume DICOMweb desde Orthanc local.

## 3.1 Principio de separación de superficies
- El **portal** decide qué estudios listar y qué acciones exponer por actor.
- **OHIF** solo visualiza estudios puntuales ya autorizados o seleccionados.
- La study list nativa de OHIF no constituye control de acceso ni debe usarse como frontera funcional para pacientes.
- Ocultar la study list nativa de OHIF es una decisión de **UX**, no una medida de seguridad real.

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
- `pacs/adapters`:
  - `SearchAdapter`
  - `RetrieveAdapter`
  - `HealthAdapter`
  - implementaciones `dicomweb`, `dimse`, `hybrid`
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
   - Ejecutar **C-MOVE** o **C-GET** entre PACS remoto y Orthanc.
4. Backend monitorea estado:
   - Por respuesta del mecanismo de orquestación en Orthanc **y/o**
   - Polling de Orthanc: verificar que el estudio aparezca completo (ver §8).
5. Al completar: `retrieve_job=done`, el UI habilita “Visualizar”.

**Principio de implementación**
- El movimiento real del estudio ocurre PACS↔PACS.
- Orthanc es el PACS local que inicia o recibe el retrieve según el mecanismo.
- El backend coordina por API y persistencia, pero no debe convertirse en proxy del payload DICOM.

**Primer slice implementado (paciente)**
- La superficie paciente expone `POST /api/patient/retrieve`.
- El backend registra el `retrieve_job` y responde `queued`; luego un worker Go asegura la modalidad remota en Orthanc y dispara `POST /modalities/{id}/get`.
- Orthanc ejecuta `C-GET` contra el PACS remoto configurado.
- El worker hace polling sobre Orthanc hasta encontrar el `StudyInstanceUID`, marca `patient_study_access.availability_status=available_local` y recién entonces habilita OHIF.
- La llamada HTTP a Orthanc para disparar `C-GET` no debe usar el timeout corto general del backend; debe respetar el deadline específico del request de retrieve.
- `GET /api/patient/studies` debe devolver `retrieve_status` por estudio para que la UI diferencie un QIDO en curso de una recuperación `C-GET` en curso.

### 5.3 Visualizar (OHIF)
1. UI abre URL de OHIF con `StudyInstanceUID` o con route de OHIF configurada.
2. OHIF consulta QIDO/WADO contra `nginx -> orthanc`.
3. Orthanc sirve instancias desde caché local.
4. El handoff actual del portal usa `GET /ohif/viewer?StudyInstanceUIDs=<uid>` para evitar caer en la study list general.
5. Las acciones de visualización del portal deben abrirse en una pestaña nueva para no perder el contexto del portal.

### 5.3.1 Flujo paciente
1. El portal valida identidad del paciente.
2. El backend compone una lista propia de estudios autorizados para ese paciente.
3. El paciente selecciona un estudio en el portal.
4. El portal abre OHIF directamente sobre ese estudio.
5. El paciente no navega la study list nativa de OHIF.
6. La implementación actual expone `POST /api/patient/search` para encolar la búsqueda remota del paciente.
7. `GET /api/patient/search?request_id=<uuid>` devuelve el estado (`queued|running|done|failed`) del worker para ese conjunto de filtros.
8. `GET /api/patient/studies?document=<dni>` queda como contrato de lectura del portal-owned list: devuelve resultados cacheados y el último estado de sync del conjunto de filtros actual, sin disparar side effects.
9. El worker actualiza `patient_study_access`, marca como `available_local` los estudios ya presentes en Orthanc y deja el resto como `pending_retrieve`.
10. El flujo debe dejar trazas estructuradas de observabilidad para:
   - solicitud de token al PACS remoto;
   - request QIDO;
   - cantidad de estudios sincronizados;
   - cantidad de estudios ya disponibles localmente;
   - duración total del sync.
11. Estas métricas no se persisten en PostgreSQL en esta etapa; se resuelven mediante logs estructurados y futuros endpoints de stats en memoria.
12. Cuando el estudio queda `pending_retrieve`, la UI del paciente ofrece un botón `Retrieve` que llama `POST /api/patient/retrieve` y recarga la lista al completar.
13. Con `patient.fake_auth = true`, `POST /api/patient/send-code` sigue validando la existencia del paciente pero omite la validación real del mail y el envío efectivo del código.
11. Si QIDO no encuentra estudios, el endpoint debe responder `200` con `studies: []`; la UI no debe tratarlo como error técnico.
12. El CTA de recarga del paciente debe expresar “Actualizar lista” o equivalente, no “Aplicar filtros”, para dejar claro que también reintenta el sync.

### 5.3.2 Flujo profesional
1. El profesional ingresa al portal mediante autenticación institucional.
2. El profesional usa un panel propio de búsqueda federada.
3. El portal muestra resultados con:
   - nodos PACS remotos;
   - disponibilidad local;
   - estado de retrieve;
   - acciones operativas.
4. El profesional dispara retrieve bajo demanda cuando corresponda.
5. El portal abre OHIF sobre el estudio puntual seleccionado.
6. La primera implementación funcional expone `GET /api/physician/results?username=<dni>` como contrato inicial del panel del profesional.
7. El primer avance operativo expone `POST /api/physician/retrieve` para disparar `C-GET` vía Orthanc REST desde la misma grilla.
8. La grilla del profesional debe recalcular `cacheStatus`, `retrieveStatus` y `viewer_url` a partir de `cached_studies`, `retrieve_jobs` y verificación real en Orthanc.
9. Sin filtros, `GET /api/physician/results` debe consultar Orthanc local en vivo y devolver todos los estudios en cache para la ventana relativa configurada en `professional.initial_cache_period`.
10. Cuando el profesional aplica filtros, `GET /api/physician/results` debe ejecutar QIDO-RS contra el nodo remoto configurado y persistir el resultado como búsqueda reciente.
11. El filtro `patient_name` del profesional debe resolverse como búsqueda fuzzy por términos normalizados; no debe requerir coincidencia literal exacta.

### 5.4 Landing pública y acceso futuro
1. El usuario accede a `/` y visualiza la landing institucional.
2. Selecciona `Paciente` o `Profesional`.
3. En MVP, el flujo es sólo visual y no genera sesión.
4. En una fase posterior:
   - `Paciente`: validación `DNI + código por mail`.
   - `Profesional`: validación `LDAP provincial + MFA`.

---

## 6) Límites de seguridad (Security Boundaries)

### 6.1 Frontera pública
- **Solo Nginx** expone puertos al exterior del compose.
- Backend y Orthanc quedan en red docker interna (salvo necesidad de DICOM).
- En desarrollo local, Postgres puede publicarse solo sobre `127.0.0.1` para inspección manual y debugging sin convertirlo en un endpoint público de red.
- Los assets del portal público deben servirse desde una ruta propia para no colisionar con los assets raíz de OHIF.
- El acceso a estudios para pacientes o médicos no debe depender solo de ocultar o mostrar la study list nativa del visor.
- La restricción futura debe validarse en backend/proxy por sesión activa del portal y `StudyInstanceUID` permitido.

### 6.2 Puertos (propuesta)
- Nginx: `8081` en desarrollo local; opcionalmente `80/443` en otros entornos.
- Orthanc DICOM (C-STORE/Move SCP): `4242` **publicado** hacia red donde están PACS remotos (si aplica).
- Orthanc HTTP: **no publicado** directamente por Nginx; el admin REST puede ligarse solo a localhost para operación local.
- Postgres: no publicado de forma pública; en desarrollo puede ligarse solo a `127.0.0.1`.
- En desarrollo local, los puertos directos de Orthanc pueden quedar ligados a `127.0.0.1` por defecto.

### 6.3 Secretos y configuración
- Variables de entorno / archivos montados (`.env`, `config.json`) fuera del código.
- No almacenar secretos en imágenes Docker ni en repositorio.
- La configuración versionada debe usar placeholders o referencias a env vars; `app/config/config.json` es local-only y no debe versionarse.

### 6.4 Auditoría técnica (MVP)
- Log estructurado (JSON) en backend.
- Persistencia mínima en Postgres:
  - requests, errores de integración, latencias, estado de jobs.
- Minimizar PHI en la auditoría técnica; si la UI requiere ver datos clínicos/demográficos, eso no obliga a persistirlos sistemáticamente en `integration_audit`.

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

### 9.1.1 Contratos ya materializados en el portal
- `GET /api/patient/studies?document=<dni>`
- `POST /api/patient/retrieve`
- `GET /api/retrieve/jobs/:id/events` (SSE)
- `GET /api/physician/results?username=<dni>&...filters`
- `POST /api/physician/retrieve`
- `GET /api/config`

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

### 10.1.1 Extensión del modelo para portal y futuras sesiones
- `patients`
  - ancla local por `document_type + document_number`
  - cache de identidad básica y timestamps de sincronización/login
- `patient_identifiers`
  - identificadores alternativos resueltos desde HIS u otros dominios
- `patient_sessions`
  - estado de sesión y verificación futura de código por mail
- `patient_study_access`
  - lista autorizada de `StudyInstanceUID` por paciente para la superficie propia del portal
- `physicians`
  - identidad local del profesional
- `physician_sessions`
  - estado de sesión y MFA futura
- `physician_recent_queries`
  - cache de búsquedas recientes del profesional
- `auth_events`
  - metadatos de autenticación, sin contraseñas en claro
- `auth_material_cache`
  - material cifrado emitido por proveedor cuando aplique; no se usa para copiar passwords

### 10.1.2 Referencia de diseño
- El contrato detallado del modelo queda en `artifacts/06_data_model.md`.
- La baseline SQL inicial queda en `app/backend/migrations/001_initial_schema.sql`.

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
  - Publica `8081` en desarrollo local.
  - Config:
    - `/api` → backend
    - `/ohif` → ohif
    - `/dicomweb` o `/dicom-web` → orthanc HTTP (limitado a DICOMweb paths)
    - `/` → UI estática

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
- En JSON guardar `*_secret_ref` o `*_env` (nombre de env var o path de archivo), no el secreto en claro.
- Backend resuelve el secreto desde env o archivo montado.
- La respuesta de `GET /api/config` debe exponer presencia de secretos (`*_present`) y referencias, pero nunca el valor real.

---

## 13) Criterios de aceptación (Acceptance Criteria)

### Infraestructura
- `docker compose up` levanta `orthanc`, `backend`, `postgres`, `nginx`, `ohif` sin pasos manuales adicionales.
- Nginx es el único punto de entrada HTTP (backend/orthanc no expuestos directamente por HTTP).
- `http://localhost:8081/` muestra la landing del portal.
- `http://localhost:8081/ohif/` redirige a la landing pública.
- `http://localhost:8081/ohif/viewer?StudyInstanceUIDs=<uid>` carga OHIF para un estudio puntual.

### Búsqueda
- Ejecutar una búsqueda dispara consultas concurrentes a **≥2** nodos configurados (cuando existan).
- La UI recibe resultados parciales vía SSE y muestra estudios deduplicados por `StudyInstanceUID`.
- Se registra en Postgres al menos: `search_requests`, `search_node_runs`, auditoría técnica.
- `GET /api/patient/studies?document=<dni>` devuelve:
  - estudios sincronizados desde el único nodo configurado en la carga inicial sin filtros;
  - `studies: []` si no hay resultados, sin convertirlo en error técnico.
- `GET /api/physician/results`:
  - sin filtros: debe consultar Orthanc local en vivo para estudios en cache de la semana actual;
  - con filtros: debe ejecutar QIDO real al nodo configurado.

### Retrieve
- Botón/endpoint `retrieve` crea job persistente.
- El job transiciona `queued → running → done/failed`.
- Al completar, el estudio queda disponible en Orthanc y OHIF puede abrirlo desde el caché local.
- El backend no debe cortar artificialmente el `C-GET` a Orthanc con el timeout corto general del cliente HTTP.
- El proxy Nginx frente a `/api/` debe tolerar retrieves largos y no cancelar `POST /api/patient/retrieve` ni `POST /api/physician/retrieve` con timeouts cortos de upstream.

### Visualización
- OHIF consume exclusivamente `/dicomweb`/`/dicom-web` (Orthanc local).
- No existe configuración en OHIF que apunte directamente a PACS remotos.
- El handoff del portal a OHIF usa un `StudyInstanceUID` explícito, no la study list general.
- La apertura del visor desde el portal ocurre en una pestaña nueva.

### Retención
- Existe mecanismo automático (cron backend o equivalente) que elimina estudios expirados (>7 días) y actualiza `cached_studies`.

### Observabilidad
- El flujo paciente deja logs estructurados para:
  - inicio de sync;
  - request de token;
  - request QIDO;
  - cantidad de estudios;
  - duración.
- Las métricas de observabilidad no se persisten en Postgres en esta etapa.

---

## 14) Decisiones explícitas de implementación (MVP)
- Streaming de resultados: **SSE**.
- Persistencia operativa: **PostgreSQL**.
- Caché de imágenes y DICOMweb: **Orthanc local**.
- Reverse proxy: **Nginx** (única exposición pública HTTP).
- Retrieve: **C-MOVE preferido**, con opción de **C-GET** si un nodo no permite C-MOVE (configurable por nodo).
- Purga: **backend cron** si Orthanc no garantiza la política requerida de forma simple.
- El flujo paciente usa lista propia del portal y retrieve manual por estudio.
- El flujo profesional usa panel propio del portal; con filtros ejecuta QIDO real al nodo remoto configurado.

---

## Open Questions Requiring Human Decision
1. **Puertos/hostnames públicos**: ¿qué hostname/puerto debe exponer Nginx en el entorno objetivo (dev/staging/on-prem)? ¿Se requiere TLS en MVP?
2. **DIMSE networking**: ¿el Orthanc local (Move SCP/C-STORE) será alcanzable desde todos los PACS remotos? (firewall/NAT/VPN). En caso contrario, ¿se permite C-GET desde Orthanc como estándar?
3. **Soporte Legacy**: para PACS sin DICOMweb, ¿se confirma uso de **dcmtk** dentro del contenedor backend (licenciamiento/instalación) o hay librería preferida?
4. **UI del MVP**: ¿se requiere UI web mínima (lista + botones) o basta con API + ejemplos (curl/Postman) para el primer hito?
5. **Estrategia de completitud**: ¿aceptan “stable window polling” como criterio de completitud de retrieve, o requieren una señal más fuerte (p.ej. conteo esperado de instancias desde metadata remota)?
6. **Metadatos en Postgres**: ¿hasta qué nivel se persiste metadata (solo studies vs series/instances) para acelerar UX y reporting técnico?
7. **HIS (ANDES)**: ¿cuál es el endpoint final y el método de autenticación para resolver identificadores alternativos de pacientes?
8. **OHIF en producción**: ¿la study list nativa quedará completamente deshabilitada para paciente y profesional, o se reservará para soporte técnico?
