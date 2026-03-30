# Portal de Imágenes

Portal web para centralizar búsqueda, recuperación y visualización de estudios DICOM desde múltiples PACS remotos, usando un Orthanc local como caché operativa y punto único de visualización para Stone Web Viewer y OHIF.

## Qué hace el portal

El portal separa tres problemas que en muchos entornos PACS quedan mezclados:

- búsqueda federada de estudios en PACS remotos
- recuperación controlada de estudios hacia una caché local
- visualización siempre desacoplada desde la caché local

La idea base es:

1. el backend consulta uno o más PACS remotos
2. el portal decide qué estudios mostrar y qué acciones habilitar
3. cuando un estudio no está local, se dispara un retrieve hacia Orthanc
4. Stone y OHIF visualizan únicamente desde Orthanc local

Eso reduce dependencia directa del visor con la latencia o disponibilidad del origen remoto y deja una superficie de operación más clara para paciente y profesional.

## Stack actual

- `nginx`: entrypoint HTTP público del stack
- `backend` en Go: búsqueda, retrieve, jobs, health, rate limit y reglas de negocio
- `postgres`: persistencia operativa
- `orthanc`: caché local y punto de retrieve / DICOMweb
- `ohif`: visor alternativo
- `stone-webviewer`: servido a través de Orthanc

## Capacidades actuales

### Acceso y superficies

- landing pública con flujos visibles de `Paciente` y `Profesional`
- branding institucional y cinta `Demo` configurable
- foco y navegación por teclado cuidados en login
- limpieza de formularios al salir o expirar sesión de shell
- timeout compartido de sesión UI configurable

### Flujo paciente

- validación de documento en frontend y backend
- prevalidación de `Enviar código`
- modos de autenticación por config:
  - `mail`
  - `fake_auth`
  - `master_key`
- `mail` es el método objetivo final de producción para paciente
- `master_key` queda como bypass transitorio y operativo mientras no esté integrado el envío/validación real del código por mail
- sesiones reales de backend para paciente
- lista propia de estudios autorizados del paciente
- búsqueda remota asíncrona con workers y estado persistido
- reutilización de búsquedas en curso para evitar duplicados
- reconciliación de búsquedas colgadas tras reinicio
- retrieve manual por estudio
- descarga DICOM cuando el estudio ya está local

### Flujo profesional

- login transitorio con validación actual configurable
- opción futura preparada para LDAP por config
- sesiones reales de backend para profesional
- carga inicial desde caché local para una ventana relativa configurable
- búsqueda remota por PACS origen seleccionado
- panel propio de resultados, sin depender de la study list nativa del visor
- retrieve manual por estudio
- descarga DICOM con límite semanal configurable

### Retrieve y caché local

- jobs de retrieve persistidos en Postgres
- retrieve real a través de Orthanc
- promoción transaccional a `available_local` solo cuando Orthanc confirma presencia local
- deduplicación de retrieves activos por `study_instance_uid`
- polling configurable del job de Orthanc para estado/progreso liviano
- scheduler opcional de precache:
  - toma estudios recientes ya presentes en `qido_study_cache`
  - encola retrieves no bloqueantes
  - usa Orthanc como destino
  - tiene configuración de intervalo, batch, antigüedad máxima y concurrencia

### Integraciones y salud operativa

- nodos PACS remotos configurables por capacidades:
  - `search`
  - `retrieve`
  - `health`
- soporte de salud remota por `auth_qido` o `dimse_c_echo`
- caché en memoria de modalidades Orthanc para no re-registrarlas todo el tiempo
- endpoint `/api/health` basado en snapshot en memoria
- SSE de salud del sistema
- modo mantenimiento servido por Nginx cuando el backend está degradado o no disponible
- rate limit liviano de login por IP e identificador
- soporte para despliegue detrás de Cloudflare + doble Nginx

### Visualización

- `Stone` como visor preferido
- `OHIF` como visor alternativo
- apertura puntual por `StudyInstanceUID`
- primera capa de handoff con grants efímeros vía `/viewer-access/<token>`
- OHIF publicado bajo `/ohif/`
- Stone publicado bajo `/stone-webviewer/`
- DICOMweb expuesto bajo `/dicom-web/`

## Estado funcional resumido

Hoy el portal ya resuelve una operación básica real:

- buscar estudios remotos
- consolidarlos en una lista propia
- traerlos a Orthanc local
- abrirlos en Stone u OHIF desde la caché
- descargar estudios locales

Todavía hay piezas transitorias o de demo, sobre todo en autenticación final y control de acceso fuerte a viewers/imágenes.

## Roadmap

### Hecho

- stack completo en Docker Compose
- landing pública y workspaces de paciente/profesional
- búsquedas paciente y profesional
- caché compartida de resultados QIDO
- enrichments ANDES iniciales bajo feature flag
- retrieve manual con estado persistido
- sesiones server-side y logout/invalidation
- endpoints paciente/profesional protegidos por sesión backend
- grants efímeros de acceso a viewer
- plugin de autorización Orthanc integrado para Stone / DICOMweb
- visualización local en Stone y OHIF
- descarga DICOM local
- health agregado + SSE + maintenance mode
- rate limit liviano de login
- scheduler opcional de precache de retrieves recientes

### Próximo

- autenticación paciente real por mail end-to-end sobre el modo final `patient.auth_mode = "mail"`
- autenticación profesional institucional real
- endurecimiento adicional de sesiones y CSRF para despliegues no locales
- invalidación de caché QIDO y refresco de enriquecimiento ANDES
- evolución de búsqueda profesional a multiorigen real
- mayor observabilidad y troubleshooting operativo

### Investigación abierta

- recuperación de PDFs asociados a prestaciones ANDES por API
- estrategia de lazy loading / integración real de OHIF
- alternativas de progreso de retrieve más ricas que el job actual de Orthanc
- posible uso de `dcmtk` o mecanismos equivalentes para mejor señal de progreso
- experimento de indexación local compartida entre backend y Orthanc

## Documentación del proyecto

### Core

- [AGENTS.md](AGENTS.md)
- [requirements.md](requirements.md)
- [decisions.md](decisions.md)
- [IMPLEMENTATION_CHECKLIST.md](IMPLEMENTATION_CHECKLIST.md)

### Artefactos

- [artifacts/01_technical_spec.md](artifacts/01_technical_spec.md)
- [artifacts/02_agent_debate.md](artifacts/02_agent_debate.md)
- [artifacts/03_implementation_plan.md](artifacts/03_implementation_plan.md)
- [artifacts/04_qa_checklist.md](artifacts/04_qa_checklist.md)
- [artifacts/05_ui_contracts.md](artifacts/05_ui_contracts.md)
- [artifacts/06_data_model.md](artifacts/06_data_model.md)

### Config y app

- [app/config/README.md](app/config/README.md)
- [app/config/config.example.json](app/config/config.example.json)

## Versionado

El proyecto usa **Semantic Versioning** con formato `MAJOR.MINOR.PATCH`.

- `MAJOR`: cambios incompatibles en contratos externos u operación esperada.
- `MINOR`: features nuevas compatibles hacia atrás.
- `PATCH`: fixes y ajustes compatibles hacia atrás.

Reglas iniciales para este repositorio:

- La versión fuente de verdad vive en [`VERSION`](VERSION).
- Mientras el producto siga en etapa inicial, la línea base arranca en `0.y.z`.
- Cambios en endpoints públicos, comportamiento de login, retrieve, viewers o deploy que rompan compatibilidad deben subir `MAJOR` cuando el proyecto pase a `1.x`.
- Cambios de configuración local no versionada no alteran por sí solos la versión; la suben solo los cambios de producto efectivamente releaseables.

## Topología runtime

```mermaid
flowchart LR
    User[Browser]

    subgraph Local["Docker Compose stack"]
        Nginx["nginx\n:8081"]
        Landing["Portal landing\nstatic UI"]
        Backend["backend\nGo API"]
        OHIF["ohif\nviewer"]
        Orthanc["orthanc\nlocal cache"]
        Postgres["postgres\nconfig + jobs"]
    end

    subgraph Remote["Remote systems"]
        RemotePACS1["Remote PACS A\nDICOMweb + DIMSE"]
        RemotePACS2["Remote PACS B\nDICOMweb + DIMSE"]
        HIS["HIS / MPI / ANDES"]
        Keycloak["Keycloak"]
    end

    User --> Nginx
    Nginx --> Landing
    Nginx --> Backend
    Nginx --> OHIF
    Nginx --> Orthanc

    Backend --> Postgres
    Backend --> Orthanc
    Backend --> HIS
    Backend --> Keycloak
    Backend --> RemotePACS1
    Backend --> RemotePACS2

    OHIF --> Orthanc
    Orthanc -. C-MOVE / C-GET / C-STORE .- RemotePACS1
    Orthanc -. C-MOVE / C-GET / C-STORE .- RemotePACS2
```

## Ejecución local

La implementación runnable vive en [`app/`](app/).

Comandos típicos:

```bash
cd /Users/psilveira/src/salud/pacs/portal2/app
docker compose up --build
```

Atajos operativos útiles dentro de [`app/`](app/):

```bash
make ps
make logs SERVICE=backend
make logs-follow SERVICE=orthanc
make logs-save SERVICE=nginx SINCE=48h
make logs-list
```

El entrypoint público del stack local es:

- `http://localhost:8081`

## Publicación detrás de Nginx externo

El proyecto puede publicarse detrás de un segundo Nginx, siempre que ese borde funcione como reverse proxy transparente.

Responsabilidades recomendadas:

- Nginx externo:
  - DNS público
  - terminación TLS
  - forwarding de IP real
- Nginx interno del proyecto:
  - routing de portal
  - fallback de mantenimiento
  - proxy a `/api/`, `/ohif/`, `/stone-webviewer/`, `/dicom-web/`
  - restricciones de rutas Orthanc

Puntos importantes:

- no publicar bajo subpath tipo `/portal/` sin adaptar la app
- no cachear `/api/`, `/ohif/`, `/stone-webviewer/`, `/dicom-web/`
- mantener SSE sin buffering
- preservar `Host`, `X-Forwarded-For`, `X-Forwarded-Proto`
- si hay Cloudflare, restaurar y forwardear IP real

## Origen del proyecto

Este repositorio arrancó como un flujo spec-driven asistido por agentes. La parte de Python, `spec.py`, `requirements-dev.txt` y los artefactos bajo `artifacts/` fueron el mecanismo inicial para disparar la especificación técnica y ordenar decisiones antes de consolidar la implementación runnable en `app/`.

La secuencia histórica fue:

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements-dev.txt
python spec.py run
```

Hoy eso queda como contexto útil para entender cómo nació la documentación y los artefactos del proyecto. No es la vía principal para operar el portal en runtime.
