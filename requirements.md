# Especificación Técnica: Portal DICOM Agregador y Caché

## 1. Visión General del Sistema
Este sistema es un portal web de imágenes médicas diseñado para centralizar el acceso a múltiples nodos PACS remotos (dcm4chee, Orthanc, Legacy). Utiliza un **PACS local de paso (Caché)** para alimentar al visor **OHIF**, garantizando una visualización rápida y desacoplada de la latencia de las redes remotas.

---

## 2. Alcance del MVP Inicial

El primer entregable debe enfocarse en una base operativa mínima. No se implementará autenticación real de usuarios finales en este primer corte, pero sí se define y expone la experiencia de ingreso pública del portal.

### 2.1 En alcance para el MVP
* **Infraestructura con Docker Compose:** levantar todos los servicios base del sistema localmente.
* **PACS local de caché:** Orthanc local, preparado para recibir estudios desde PACS remotos.
* **Backend en Go:** servicio agregador para consultar nodos remotos, gestionar retrieves y exponer endpoints internos del portal.
* **Base de datos de aplicación:** PostgreSQL para almacenar configuración operativa, jobs de retrieve, auditoría técnica y metadatos locales.
* **Nginx:** servir contenido estático y actuar como reverse proxy para backend y visor.
* **Landing pública del portal:** página inicial servida por Nginx con branding **RedImagenesNQN** e identidad visual inspirada en **ANDES**.
* **Experiencia de ingreso pública:** selector visual de perfil `Paciente` / `Profesional`.
* **Responsive móvil:** la landing pública y las superficies del portal deben ser utilizables en teléfonos y tablets, con layout adaptativo y controles táctiles cómodos.
* **Integración con HIS:** el sistema debe permitir configurar credenciales, API keys, URLs base y parámetros necesarios para futuras consultas al HIS.
* **Configuración de PACS remotos:** el sistema debe permitir cargar detalles de conexión para nodos dcm4chee remotos.
* **Visualización desacoplada:** OHIF debe consumir estudios desde el Orthanc local y no desde los PACS remotos.
* **Portal assets propios:** el logo, favicon y assets de la landing deben ser servidos por Nginx sin mezclarse con los assets del contenedor OHIF.

### 2.2 Fuera de alcance en este MVP
* Login real de médicos.
* Login real de pacientes.
* OTP real por SMS o Email.
* Gestión de sesiones.
* JWT para restricción de acceso a OHIF.
* Permisos finos por usuario.
* Share links o links temporales.
* Integración real con LDAP provincial.
* Implementación real de MFA para médicos.

---

## 3. Modelos de Acceso y Autenticación

### 3.1 Flujo público visible en MVP: Ingreso de Médicos
* **UI visible en MVP:** formulario visual con `DNI / usuario` y `contraseña`.
* **Estado actual:** sólo maqueta funcional de interfaz; no hay autenticación real ni sesión.
* **Objetivo de integración posterior:** autenticación contra **LDAP provincial** y segundo factor **MFA** para médicos.
* **Alcance funcional futuro:** acceso a una consola propia del portal con búsqueda manual mediante filtros, estado federado por PACS remoto, disponibilidad local, estado de retrieve y apertura puntual en el visor.

### 3.2 Flujo público visible en MVP: Ingreso de Pacientes
* **UI visible en MVP:** formulario visual con `Documento`, acción `Enviar OTP` e ingreso de `Código OTP`.
* **Estado actual:** sólo maqueta funcional de interfaz; no hay OTP real ni sesión.
* **Objetivo de integración posterior:** validación de `DNI + OTP` vía SMS/Email.
* **Identidad (HIS Integration):** en fase posterior, el sistema consultará un servicio REST del HIS para obtener los identificadores asociados al DNI del paciente.
* **Búsqueda Implícita futura:** al validar correctamente el ingreso de paciente, el portal armará una lista propia de estudios autorizados para ese paciente.
* **Restricción funcional futura:** el paciente no debe navegar la base completa del caché local ni la lista nativa de estudios de OHIF.

---

## 4. Motor de Búsqueda Concurrente (Go Workers)

El backend, desarrollado en **Go**, gestiona las consultas en paralelo para evitar cuellos de botella.

### 4.1 Arquitectura de Handlers
Se implementa una interfaz `DICOMHandler` para abstraer la complejidad de cada nodo:
1. **QIDO-RS Handler:** Para nodos modernos (dcm4chee-arc, Orthanc).
2. **C-FIND Handler:** Para nodos Legacy sin API REST (vía librería nativa o dcmtk).
3. **Local Handler:** Para verificar disponibilidad inmediata en el caché local.

### 4.2 Lógica de Agregación y Fallback
* **Deduplicación:** Si un `StudyInstanceUID` se encuentra en múltiples nodos, el sistema lo unifica en la UI, manteniendo una lista de locaciones.
* **Jerarquía de Nodos:** Se define una prioridad por nodo. El sistema preferirá recuperar imágenes del nodo con mayor prioridad o menor latencia histórica.
* **Streaming de Resultados:** Uso de **WebSockets** o **SSE** para enviar resultados parciales a la UI a medida que los workers de Go responden.

---

## 5. Gestión del Ciclo de Vida del Estudio

### 5.1 Recuperación (Retrieve)
* **Botón "Retrieve":** Dispara un comando `C-MOVE` o `C-GET` desde el PACS remoto seleccionado hacia el **PACS Local (Orthanc)**.
* **MVP:** El retrieve se dispara manualmente o mediante endpoint interno controlado por backend.
* **Fase posterior:** Para pacientes, el sistema dispara automáticamente el retrieve de los estudios de los últimos 30 días tras un login exitoso.
* **Principio de implementación:** el intercambio real de estudios debe ocurrir entre PACS, con Orthanc actuando como PACS local y par DICOM/DICOMweb de los nodos remotos.
* **Backend:** coordina, dispara y monitorea el retrieve, pero no debe transformarse en proxy del payload DICOM como camino normal de transferencia.
* **Legacy o REST:** mientras no aparezca una limitación concreta, los intercambios de estudios deben resolverse como comunicación PACS↔PACS entre Orthanc y los remotos, ya sea por DIMSE legacy o por mecanismos DICOM REST del producto remoto.
* **Primer retrieve funcional de paciente:** el portal expone `POST /api/patient/retrieve` y, por ahora, dispara `C-GET` sobre Orthanc REST contra el único nodo remoto configurado para estudios marcados como `pending_retrieve`.

### 5.2 PACS Local (Caché)
* **Tecnología:** Orthanc (Ligero, API REST potente).
* **Retención:** Política de purga automática de **7 días**. 
* **Configuración:** Debe actuar como `Move SCP` para recibir estudios de los nodos remotos.

---

## 6. Visualización (OHIF Viewer)

* **Integración:** Una vez que el estudio está en el PACS local, el botón "Visualizar" abre OHIF.
* **Configuración del Visor:** OHIF consume datos únicamente del PACS local mediante protocolos DICOMweb (`WADO-RS`).
* **Aislamiento:** El visor no conoce la existencia de los PACS remotos, solo interactúa con el caché.
* **Ruta de publicación:** OHIF se publica bajo `/ohif/` y consume el DICOMweb local bajo `/dicom-web/`.
* **Rol del visor:** OHIF debe comportarse como visor puntual de estudios/series autorizados, no como superficie principal de búsqueda o navegación.
* **Listado de estudios:** la study list nativa de OHIF es una decisión de UX y no debe considerarse un mecanismo de restricción de acceso.
* **Pacientes:** no deben usar la study list nativa de OHIF. Deben ver una lista propia del portal con sus estudios autorizados.
* **Médicos:** no deben depender de la study list nativa de OHIF como workflow principal. Deben usar un panel propio del portal con búsqueda, estado federado y retrieve.

## 6.1 Superficies de UI futuras

### 6.1.1 Superficie Paciente
* Lista propia del portal con estudios autorizados del paciente.
* Posibles filtros simples orientados a experiencia de paciente:
  * fecha;
  * modalidad;
  * descripción resumida;
  * estado de disponibilidad.
* Acción principal: abrir un estudio puntual en OHIF.
* La experiencia debe ser responsive y usable en móvil.
* El contrato explícito de esta superficie queda definido en `artifacts/05_ui_contracts.md`.
* En el mock actual del portal, el ingreso de paciente debe aterrizar primero en esta superficie y no redirigir directamente a la home general de OHIF.
* La primera implementación funcional de esta superficie consume `GET /api/patient/studies?document=<dni>` y renderiza la lista desde datos del backend.
* En la carga inicial sin filtros, ese endpoint debe ejecutar `QIDO-RS /studies?PatientID=<dni>` contra el único nodo PACS configurado, persistir los estudios sincronizados y reutilizar esa cache local para los filtros posteriores del paciente.
* Los estudios remotos de esta lista deben quedar inicialmente como `pending_retrieve`, con botón explícito `Retrieve` para traerlos a Orthanc antes de habilitar `Visualizar estudio`.

### 6.1.2 Superficie Médico
* Panel propio del portal para búsqueda federada y operación.
* Debe incluir, como mínimo:
  * filtros de búsqueda;
  * lista de resultados;
  * origen o nodos PACS remotos;
  * disponibilidad local en caché;
  * estado de retrieve;
  * acción de retrieve;
  * acción de visualización puntual en OHIF.
* La experiencia debe ser responsive y usable en móvil, al menos para consulta y validación rápida.
* El contrato explícito de esta superficie queda definido en `artifacts/05_ui_contracts.md`.
* En el mock actual del portal, el ingreso profesional debe aterrizar primero en esta superficie y no redirigir directamente a la home general de OHIF.
* La primera implementación funcional de esta superficie consume `GET /api/physician/results?username=<dni>` y renderiza resultados desde búsquedas recientes sembradas en la base hasta integrar la búsqueda federada real.

---

## 7. Infraestructura Base del MVP

### 7.1 Servicios mínimos en Docker Compose
El entorno de desarrollo del MVP debe incluir:
* `orthanc`: PACS local de caché.
* `backend`: servicio Go agregador y coordinador.
* `postgres`: persistencia operativa del sistema.
* `nginx`: reverse proxy y servicio de contenido estático.
* `ohif`: visor conectado al Orthanc local.

### 7.1.1 Estado operativo actual del stack
* La imagen del visor usada debe ser `ohif/app` con tag fijo, evitando `latest`.
* El stack actual utiliza `ohif/app:v3.11.1`.
* Nginx publica el portal y el visor sobre `http://localhost:8080`.
* El HTTP admin de Orthanc puede estar disponible sólo para localhost cuando se requiera operación local.

### 7.2 Persistencia esperada en PostgreSQL
La base debe contemplar como mínimo:
* configuración de nodos remotos;
* jobs y estado de retrieves;
* auditoría técnica y logs de integración;
* cache o referencias locales de estudios sincronizados;
* configuración del HIS necesaria para futuras consultas.

### 7.3 Configuración externa requerida
El sistema debe estar preparado para recibir por configuración:
* API key o credenciales equivalentes del HIS;
* URL base y parámetros del HIS;
* AE Title, host, puerto, base URL DICOMweb y credenciales de cada PACS remoto dcm4chee;
* host de Keycloak, realm, `client_id` y `client_secret` para obtener tokens OAuth2 de los PACS remotos cuando aplique;
* parámetros del Orthanc local;
* settings de Nginx para exponer backend, visor y estáticos.
* settings separados para assets del portal público, para evitar colisiones con assets servidos por OHIF.

---

## 8. Especificación de Configuración (JSON)

```json
{
  "pacs_nodes": [
    {
      "id": "sede_central",
      "name": "DCM4CHEE Central",
      "protocol": "qido_rs",
      "priority": 1,
      "timeout_ms": 5000,
      "ae_title": "CENTRAL_PACS"
    },
    {
      "id": "sede_norte",
      "name": "Orthanc Norte",
      "protocol": "qido_rs",
      "priority": 2,
      "timeout_ms": 3000,
      "ae_title": "NORTE_PACS"
    }
  ],
  "cache_config": {
    "provider": "orthanc_local",
    "retention_days": 7,
    "max_disk_usage_gb": 500
  }
}
```
## 9. Auditoría y Seguridad
### 9.1 MVP
* Registro técnico de consultas, retrieves, errores de integración y sincronización.
* Secretos y credenciales manejados por variables de entorno o archivos de configuración fuera del código.
* Los archivos de configuración versionados deben contener placeholders o referencias a variables de entorno; los valores locales de runtime no deben commitearse.
* La base debe poder persistir cache de pacientes, identificadores alternativos provenientes del HIS, estudios conocidos por `StudyInstanceUID`, búsquedas recientes de profesionales y estado de sesión.
* Las credenciales de médicos no deben almacenarse en claro. Solo pueden persistirse eventos de autenticación, estado de sesión y material de proveedor cifrado cuando exista una necesidad real de integración.
* La exposición pública del stack debe pasar por Nginx.
* Los puertos directos de Orthanc deben poder limitarse a `127.0.0.1` para operación local.
* Para PACS remotos dcm4chee, el backend debe poder obtener un token OAuth2 por `client_credentials` contra Keycloak y reutilizarlo para invocar la API REST del archivo.
* Por ahora, las métricas de observabilidad no deben persistirse en PostgreSQL. Deben resolverse mediante logs estructurados y, si hiciera falta, endpoints de stats en memoria.

### 9.2 Fase posterior
* Registro de cada DNI consultado, por qué usuario (Médico/Paciente) y desde qué IP.
* El acceso a OHIF debe estar vinculado a la sesión activa del portal.
* Implementación de JWT en el proxy de imágenes para restringir el acceso a nivel de StudyInstanceUID en OHIF.
* La autorización efectiva no debe depender de ocultar la study list del visor: el backend/proxy debe validar sesión activa + `StudyInstanceUID` permitido en cada acceso de viewer e imágenes.

---

## 10. Contrato de Integración con dcm4chee-arc-light

### 10.1 Fuente de verdad de la API remota
Para el MVP, la integración con PACS remotos dcm4chee debe tomar como contrato la especificación OpenAPI/Swagger oficial provista por dcm4chee-arc-light.

### 10.2 Capacidades usadas en el MVP
El MVP debe apoyarse en estas capacidades del PACS remoto:
* **QIDO-RS** para búsqueda de estudios.
* **WADO-RS** para consultas de validación o compatibilidad si hiciera falta, aunque OHIF seguirá consumiendo desde Orthanc local.
* **MOVE / C-MOVE** para recuperar estudios hacia el Orthanc local.

### 10.3 Capacidades fuera del primer slice
Estas capacidades del Swagger de dcm4chee no son prioridad del primer slice:
* PAM-RS para gestión administrativa de pacientes;
* operaciones de cambio de Patient ID;
* operaciones de rechazo, merge o mantenimiento administrativo del archivo remoto.

### 10.4 Autenticación esperada para dcm4chee
Cuando un PACS remoto esté protegido por Keycloak, el backend debe:
1. solicitar un `access_token` vía `grant_type=client_credentials`;
2. usar `client_id` y `client_secret` provistos por configuración;
3. enviar el token como `Authorization: Bearer <token>` en las llamadas REST al PACS remoto.

### 10.5 Ejemplo de patrón de autenticación remota
Patrón de referencia provisto para el entorno:

```bash
TOKEN=$(curl -s -k \
  --data "grant_type=client_credentials&client_id=${CLIENT_ID}&client_secret=${CLIENT_SECRET}" \
  https://${KEYCLOAK_HOST}/auth/realms/dcm4che/protocol/openid-connect/token \
  | jq '.access_token' | tr -d '"')
```

Luego el backend debe reutilizar ese token para invocar endpoints REST del archivo remoto bajo una ruta tipo:

```text
https://${PACS_HOST}/dcm4chee-arc/aets/${AET}/rs/...
```
