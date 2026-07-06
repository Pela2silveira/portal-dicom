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
* **Fallback de mantenimiento:** si el backend no está operativo, Nginx debe responder la landing del portal con una página estática de mantenimiento y contacto institucional.
* **Arranque degradado del backend:** si Postgres, Mongo, Orthanc o la carga de configuración fallan al inicio, el backend no debe abortar el proceso; debe quedar levantado, exponer `/api/health` degradado y permitir que Nginx sirva mantenimiento.
* **Separación liveness/readiness:** el contenedor backend debe exponer un endpoint liviano de liveness para Docker Compose y reservar `/api/health` para estado operativo degradado/listo.
* **Salud por componente:** `/api/health` debe distinguir componentes `required` y `optional`, de modo que sólo los requeridos dejen la app `unavailable`; los opcionales deben degradar capacidad sin forzar mantenimiento global.
* **Health remoto no bloqueante:** los checks de PACS remotos no deben ejecutarse sincrónicamente en cada `GET /api/health`; el endpoint debe responder desde un snapshot en memoria actualizado en background para no sobrecargar Orthanc ni degradar la landing.
* **Eventos de salud del sistema:** el backend debe exponer un stream SSE para cambios de estado operativo, de modo que la app abierta pueda volver a la landing de mantenimiento y la landing pueda recargarse cuando el sistema se recupere.
* **Retorno suave al landing por salud:** si la UI abierta recibe `unavailable` desde el stream de salud o detecta `/api/health = 503`, debe volver al landing sin navegación completa del browser, preservando una experiencia sin parpadeo ni hard reload.
* **Frecuencia de chequeo de salud:** el watcher agregado de componentes debe ejecutarse con una cadencia conservadora de 1 minuto; la conexión SSE puede mantenerse viva con heartbeats más frecuentes sin volver a ejecutar todos los checks.
* **Minimización de exposición pública:** los endpoints públicos de estado deben exponer sólo la información mínima necesaria para la UX o la operación de borde; no deben filtrar detalles de componentes internos, rutas locales, config paths ni mensajes operativos sensibles.
* **Protección de endpoints internos:** rutas como `/api/config` no deben quedar expuestas por la superficie pública de Nginx.
* **Componentes requeridos actuales:** `backend`, `postgres`, `orthanc`, `mongo_identity` y `config`.
* **Componentes opcionales actuales:** nodos `remote_pacs`.
* **Extensibilidad prevista:** el modelo de componentes debe admitir futuros ítems como `ldap_auth` y `mail_delivery`, junto con workflows de contingencia específicos.
* **Landing pública del portal:** página inicial servida por Nginx con branding **RedImagenesNQN** e identidad visual inspirada en **ANDES**.
* **Experiencia de ingreso pública:** selector visual de perfil `Paciente` / `Profesional`.
* **Responsive móvil:** la landing pública y las superficies del portal deben ser utilizables en teléfonos y tablets, con layout adaptativo y controles táctiles cómodos.
* **Protección de entradas:** todo campo editable del portal debe aplicar saneamiento/normalización acorde a su tipo en frontend y repetir validación defensiva equivalente en backend antes de tocar búsquedas, retrieves, autenticación o descargas.
* **Integración con HIS:** el sistema debe permitir configurar credenciales, API keys, URLs base y parámetros necesarios para futuras consultas al HIS.
* **Excepción transitoria para identidad de paciente:** mientras no esté disponible la API REST del HIS, el backend podrá consultar una base MongoDB de forma directa únicamente para lectura de identidad de paciente y resolución de identificadores.
* **Colección inicial Mongo:** el adapter temporal consulta la colección `paciente` y normaliza `_id`, `documento`, datos demográficos y el primer email activo si existe.
* **Email de contacto del paciente (precedencia):** para el envío del código de acceso, el email principal es el de la cuenta de la colección `pacienteApp` cuyo array `pacientes` referencia al `_id` del paciente con `relacion = "principal"` (campo `email` en la raíz de `pacienteApp`); el primer email activo de `paciente.contacto` queda como fallback. Una falla al consultar `pacienteApp` no debe bloquear el envío cuando exista email de contacto.
* **Persistencia local de éxito:** toda resolución exitosa de identidad de paciente desde Mongo debe persistirse en Postgres (`patients` + `patient_identifiers`) para reutilización operativa posterior.
* **Búsqueda remota por identificadores alternativos:** el flujo de búsqueda del paciente debe consultar QIDO remoto usando el DNI canónico y, cuando exista, el string del `_id` Mongo persistido como `mongo_object_id`, deduplicando resultados por `StudyInstanceUID`.
* **Búsqueda paciente multi-PACS:** cuando existan varios nodos con `search.mode = qido_rs`, el flujo paciente debe consultar todos los nodos elegibles y consolidar los estudios en una única lista autorizada.
* **Configuración de PACS remotos:** el sistema debe permitir cargar detalles de conexión para nodos dcm4chee remotos.
* **Capacidades por nodo PACS:** la configuración de cada nodo debe distinguir al menos `search`, `retrieve` y `health`, para soportar combinaciones `dicomweb`, `dimse` e `hybrid`.
* **Health remoto por capacidad:** el modo `health` de un nodo debe poder definirse al menos como `auth_qido` o `dimse_c_echo`.
* **Visualización desacoplada:** OHIF debe consumir estudios desde el Orthanc local y no desde los PACS remotos.
* **Portal assets propios:** el logo, favicon y assets de la landing deben ser servidos por Nginx sin mezclarse con los assets del contenedor OHIF.

### 2.2 Fuera de alcance en este MVP
* Login real de médicos.
* Login real de pacientes.
* Código real por mail.
* Gestión de sesiones.
* JWT para restricción de acceso a OHIF.
* Permisos finos por usuario.
* Share links o links temporales.
* Integración real con LDAP provincial.
* Implementación real de MFA para médicos.

---

## 3. Modelos de Acceso y Autenticación

### 3.1 Flujo público visible en MVP: Ingreso de Médicos
* **UI visible en MVP:** formulario visual con `DNI` y `contraseña`.
* **Estado actual:** el portal valida el ingreso profesional contra la colección Mongo `profesional` cuando `his.provider = his_mongo_direct`, pero todavía no existe autenticación real provincial ni sesión final.
* **Objetivo de integración posterior:** autenticación contra **LDAP provincial** y segundo factor **MFA** para médicos.
* **Alcance funcional futuro:** acceso a una consola propia del portal con búsqueda manual mediante filtros, estado federado por PACS remoto, disponibilidad local, estado de retrieve y apertura puntual en el visor.
* **Resumen de salud PACS en panel profesional:** el panel profesional debe mostrar una leyenda `PACS en línea X/Y` calculada sobre los health checks remotos configurados, con detalle por hover/focus de nodos online y offline usando el nombre visible del nodo.
* **Selector de origen en búsqueda profesional:** el panel profesional debe ofrecer un selector de origen con `Cache local` y los PACS remotos actualmente online para que la búsqueda remota consulte solo un nodo explícitamente elegido y no dispare consultas federadas innecesarias.
* **Feature flag operativa de auth profesional:** el backend debe permitir alternar rápidamente entre el modo transitorio actual y el modo institucional futuro mediante `professional.fake_auth` en `config.json`, con default `true`.
* **Ventana inicial del panel profesional:** el backend debe permitir definir en `config.json` el período relativo usado para la carga inicial del profesional sin filtros mediante `professional.initial_cache_period`.
* **Rate limit de descarga profesional:** el backend debe permitir definir en `config.json` un máximo semanal de descargas completas `ZIP DICOM` por profesional mediante `professional.weekly_download_limit`, con valor operativo inicial de `100`.
* **Excepciones de acceso profesional:** `config.json` puede definir `professional.license_exceptions` como una lista explícita de DNI/username habilitados a ingresar salteando tanto la matrícula activa como `habilitado == true`.
* **Semántica del modo falso profesional:** con `professional.fake_auth = true`, el backend mantiene la validación operativa actual contra Mongo `profesional`; con `false`, `POST /api/physician/login` debe autenticar por LDAP antes de aplicar la autorización en Mongo.
* **Primer slice LDAP profesional:** con `professional.fake_auth = false`, el login profesional debe intentar autenticación LDAP directa usando `LDAP_HOST`, `LDAP_PORT` y `LDAP_OU`, con DN `uid=<dni>,<LDAP_OU>` sobre conexión insegura `ldap://`; Mongo sigue siendo la fuente de autorización y matrícula.
* **Restricción funcional actual:** salvo los DNI/username incluidos en `professional.license_exceptions`, sólo se permite el ingreso si el profesional existe, `habilitado == true`, `profesionalMatriculado == true` y tiene una matrícula profesional en Mongo.
* **Criterio de matrícula profesional:** el backend debe leer `formacionGrado[].matriculacion[]` y tomar la primera entrada con `baja.fecha == null`, usando `matriculaNumero` como número visible.
* **Demográficos visibles del profesional:** `nombre y apellido`, `DNI` y `número de matrícula`.
* **Semántica de búsqueda profesional actual:** sin filtros adicionales, `source=local_cache` debe consultar Orthanc local en vivo dentro de la ventana configurada; con filtros, `source=local_cache` debe filtrar sobre la cache local y `source=<node_id>` debe consultar únicamente ese PACS remoto. El flujo paciente sigue siendo multi-PACS porque ya acota por identidad del paciente.
* **Búsqueda profesional por DNI vs ID (v0.7.0):** el formulario expone dos campos independientes: `DNI del paciente` (parámetro `document_number`) e `ID DICOM (directo)` (parámetro `patient_id`). La búsqueda por ID se envía tal cual como `PatientID` DICOM al origen elegido (sin resolución). La búsqueda por DNI resuelve los identificadores del paciente (primero en la cache `patient_identifiers`, refrescando desde Mongo/HIS cuando falta el `mongo_id`) y, por cada nodo, aplica la regla de mapeo `patient_id_source` de su configuración (`dni` por defecto, `mongo_id`, o un campo de otro provider a futuro) para decidir qué identificador viaja como `PatientID`. Si un nodo no puede mapear el DNI a su identificador, no devuelve resultados en lugar de enviar un valor incorrecto. En `local_cache`, la búsqueda por DNI consulta Orthanc por cada identificador mapeado distinto entre los nodos y deduplica por `StudyInstanceUID`.
* **Reconexión del provider Mongo:** cuando `his.provider = his_mongo_direct` y la conexión inicial falle, el backend debe reintentar la conexión al menos cada 1 minuto sin requerir reinicio del contenedor.
* **Salud continua del provider Mongo:** si la conexión Mongo se pierde después de un arranque exitoso, `/api/health` debe degradarse también durante la caída y recuperarse cuando el provider vuelva a responder.

### 3.2 Flujo público visible en MVP: Ingreso de Pacientes
* **UI visible en MVP:** formulario visual con `Documento`, acción `Enviar código` e ingreso de `Código por mail`.
* **Estado actual:** existe sesión backend real y validación de acceso transitoria; aún no hay envío real ni verificación one-time-code por mail.
* **Objetivo de integración posterior:** completar la validación final de `DNI + código por mail`.
* **Identidad (HIS Integration):** en fase posterior, el sistema consultará un servicio REST del HIS para obtener los identificadores asociados al DNI del paciente.
* **Transición táctica previa:** hasta contar con esa API REST, se admite un adaptador backend-only hacia MongoDB para lectura directa de identidad, siempre encapsulado detrás de una abstracción reemplazable.
* **Restricción obligatoria del adaptador Mongo:** debe ser estrictamente `read-only`; no puede escribir, mutar ni administrar estructuras en MongoDB.
* **Restricción de performance del adaptador Mongo:** las consultas deben ser performantes, acotadas, con proyecciones mínimas y apoyadas en índices adecuados; no se admiten collection scans como base del flujo normal.
* **Búsqueda Implícita futura:** al validar correctamente el ingreso de paciente, el portal armará una lista propia de estudios autorizados para ese paciente.
* **Validación actual al enviar código:** el portal llama al backend para verificar que el paciente exista y que tenga un mail activo antes del envío real del correo.
* **Modo de auth paciente por config:** el backend debe permitir alternar el acceso paciente mediante `patient.auth_mode` en `config.json`, con valores al menos `mail`, `fake_auth` y `master_key`.
* **Método final esperado para paciente:** `patient.auth_mode = "mail"` es el camino final de producción.
* **Semántica del modo `fake_auth`:** con `patient.auth_mode = "fake_auth"`, el backend mantiene la validación de existencia del paciente pero omite la validación real del mail y el envío efectivo del código.
* **Semántica del modo `master_key`:** con `patient.auth_mode = "master_key"`, el backend valida la existencia del paciente y habilita un acceso común basado en una llave maestra configurada en `patient.master_key`; este modo es transitorio y no reemplaza el diseño final por mail.
* **Validación efectiva en `Continuar`:** el flujo paciente debe validar el ingreso al confirmar `Continuar`; en modo `master_key` el código ingresado debe compararse contra `patient.master_key`, mientras que la visual del login permanece estable para demos.
* **Entrada oculta para `master_key`:** cuando `patient.auth_mode = "master_key"`, el campo `Código por mail` debe comportarse como ingreso oculto con asteriscos en la UI, sin cambiar el resto de la visual del flujo.
* **Expiración de sesión configurable:** la sesión del portal debe expirar para paciente y profesional según `portal.session_timeout_minutes` en `config.json`; el valor por defecto actual es 10 minutos.
* **Autorización backend obligatoria por sesión:** endpoints protegidos de paciente y profesional deben resolver identidad/autorización desde la sesión server-side activa, no desde parámetros `document` o `username` enviados por el cliente.
* **Config pública mínima:** la landing pública no debe leer `config.json` completo ni depender de `/api/config`; solo puede consumir un endpoint mínimo de runtime (`/api/runtime-config`) con los valores estrictamente necesarios para la UI principal, comenzando por `portal.session_timeout_minutes`.
* **Cinta demo configurable:** la marca diagonal `Demo` debe habilitarse mediante `portal.show_demo_ribbon` y, cuando esté activa, debe aparecer de forma consistente en la landing pública, el workspace de paciente y el workspace de profesional.
* **Progreso visible de retrieve:** la UI de paciente y profesional debe mostrar un progreso liviano del retrieve basado en estado/fase del job y porcentaje aproximado cuando Orthanc lo informe, sin convertir la grilla en un polling agresivo.
* **Polling configurable de progreso:** la frecuencia con que el backend consulta el progreso del job de Orthanc para retrieves activos debe configurarse mediante `portal.retrieve_progress_poll_seconds`; el valor operativo inicial es 5 segundos.
* **Precaching configurable de estudios recientes:** el backend debe poder ejecutar un job periódico no bloqueante que encole retrieves automáticos para estudios recientes presentes en la lista/caché QIDO y todavía no locales; su activación, intervalo, antigüedad máxima, batch por ciclo y concurrencia de workers deben configurarse desde `portal`.
* **TO-DO de estrategia alternativa para progreso real de retrieve:** si el porcentaje informado por Orthanc sigue siendo demasiado pobre para UX, evaluar un handler propio basado en `dcmtk` u otro mecanismo que permita observar progreso más granular y potencialmente más performante durante la transferencia.
* **TO-DO de indexación local compartida:** también debe evaluarse si una estrategia combinada con el plugin de indexación local de Orthanc, compartiendo un volumen entre backend y contenedor Orthanc, puede mejorar observabilidad de ingreso local y soporte de progreso sin penalizar demasiado la arquitectura.
* **Rate limit liviano de login:** los endpoints `POST /api/patient/send-code`, `POST /api/patient/login` y `POST /api/physician/login` deben aplicar rate limiting liviano por IP efectiva y por identificador normalizado (`documento`/`dni`), devolviendo `429` y `Retry-After` cuando se exceden los intentos.
* **Mensajes funcionales requeridos:** si el paciente no tiene mail activo, la UI debe indicar que concurra a su centro de salud más cercano para actualizar los datos de contacto; si el paciente no existe, debe informar que el paciente no cuenta con registros.
* **Confirmación visible de destinatario:** cuando el paciente tenga mail registrado y solicite el código, la UI debe mostrar el correo ofuscado en el mensaje de confirmación, preservando los primeros 3 caracteres del local-part y ocultando desde el cuarto hasta `@`.
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

## 6. Visualización (Stone + OHIF)

* **Integración:** Una vez que el estudio está en el PACS local, el portal debe ofrecer dos acciones de visualización: `Visualizar estudio` (preferida, usando Stone Web Viewer de Orthanc) y `Visualizar con OHIF Viewer` (alternativa).
* **Preferencia de visor:** Stone es el visor preferido en la grilla por su UX liviana y por habilitar futuras descargas derivadas (`PDF/JPG`) más cercanas al caché local.
* **Configuración del Visor:** tanto Stone como OHIF consumen estudios únicamente desde el PACS local Orthanc; ningún visor debe consultar PACS remotos en forma directa.
* **Aislamiento:** los visores no conocen la existencia de los PACS remotos, solo interactúan con el caché.
* **Ruta de publicación:** OHIF se publica bajo `/ohif/`, Stone bajo `/stone-webviewer/`, y ambos operan sobre el Orthanc local.
* **Rol del visor:** los visores deben comportarse como superficies puntuales de visualización de estudios/series autorizados, no como superficies principales de búsqueda o navegación.
* **Handoff actual al visor:** el portal debe abrir Stone u OHIF con `StudyInstanceUID` explícito para evitar que el paciente caiga en una study list general.
* **Listado de estudios:** la study list nativa de OHIF y cualquier navegación general de visor son decisiones de UX y no deben considerarse mecanismos de restricción de acceso.
* **Pacientes:** no deben usar la study list nativa de OHIF. Deben ver una lista propia del portal con sus estudios autorizados.
* **Etiqueta de origen PACS en resultados:** el listado de paciente y el panel profesional deben indicar el hospital/sede de origen usando el `name` configurado del nodo DICOM/PACS, no solo identificadores técnicos.
* **Enriquecimiento ANDES en resultados:** cuando la feature flag `his.prestaciones_enrichment_enabled` esté activa y exista correspondencia en Mongo `prestaciones`, las tarjetas de paciente y profesional deben mostrar `Prestación en ANDES` y `Profesional en ANDES` a partir del `StudyInstanceUID` DICOM (`metadata.pacs-uid`). En flujo paciente el match debe usar además el identificador de paciente resuelto desde ANDES/Mongo; en profesional debe usar el `andes_organization_id` configurado en el nodo PACS y la fecha del estudio. La flag debe nacer deshabilitada por defecto.
* **Persistencia compartida de resultados QIDO:** los metadatos de estudios obtenidos por QIDO remoto deben persistirse en PostgreSQL como una cache compartida por `StudyInstanceUID + nodo PACS`, independiente de si el estudio se observó desde el flujo de paciente o de profesional.
* **Persistencia del enriquecimiento ANDES:** cuando exista enriquecimiento ANDES para un `StudyInstanceUID + nodo PACS`, esa información debe persistirse junto con la cache QIDO para reutilización posterior sin volver a consultar siempre la fuente externa.
* **TO-DO de PDFs ANDES por API:** queda pendiente investigar y definir la recuperación de PDFs asociados a prestaciones ANDES por API, incluyendo endpoint, autenticación, contrato binario/URL y criterios de exposición en los flujos de paciente y profesional.
* **TO-DO de invalidación:** queda pendiente definir el mecanismo funcional y operativo para invalidar o purgar elementos de esa cache cuando un estudio deje de estar disponible en un PACS o cuando el enriquecimiento ANDES requiera refresco.
* **Transacción de disponibilidad local:** el cambio de `pending_retrieve` a `available_local` debe ocurrir únicamente después de que Orthanc confirme la presencia local del estudio, y debe persistirse en la misma transacción que el cierre exitoso del retrieve y la actualización de cache local.
* **Verificación de completitud del retrieve:** tras un C-GET exitoso, el backend no debe marcar `local_complete` solo por la presencia del estudio en Orthanc; debe verificar la completitud comparando, a nivel serie, las series/instancias esperadas por el PACS de origen contra las presentes localmente. Si faltan series o instancias, el estudio debe marcarse `local_partial`. Cuando el origen no informe conteos confiables, la verificación degrada a best-effort (se mantiene el comportamiento previo). La verificación es best-effort: su fallo no debe hacer fallar un retrieve por lo demás exitoso.
* **No bloquear estudios incompletos:** un estudio `local_partial` debe seguir siendo visualizable y descargable (se abre lo que ya está presente); el estado parcial habilita la remediación en background y su señalización en la UI, pero no bloquea las operaciones.
* **Remediación de estudios parciales:** ante un `local_partial`, el backend debe reintentar automáticamente **solo las series faltantes** (sin retrabajar lo ya traído), re-verificando completitud entre intentos. La cantidad de intentos, el backoff entre reintentos y el timeout total del retrieve deben ser configurables en `config.json` (`portal.retrieve_max_attempts` con valor inicial 3, `portal.retrieve_retry_backoff_seconds`, `portal.retrieve_timeout_minutes`).
* **TO-DO de contrato multiorigen:** cuando profesional evolucione a multiselect de PACS, el contrato lógico de resultados debería exponer `source_node_ids[]` como array agregado por `StudyInstanceUID`, manteniendo la persistencia física por `StudyInstanceUID + nodo PACS`.
* **TO-DO de integración/lazy loading de OHIF:** primero debe confirmarse si el stack actual usa únicamente integración estándar `dicom-web` contra Orthanc local o si interviene algún plugin específico de Orthanc; recién después corresponde evaluar optimizaciones de carga perezosa o precálculo de metadata y su impacto en primera apertura, reaperturas y costo operativo.
* **Descarga de estudio:** tanto paciente como profesional deben poder descargar el estudio completo local en formato `ZIP DICOM` desde Orthanc cuando ya esté disponible en caché local.
* **Límite de descarga profesional:** las descargas `ZIP DICOM` iniciadas por profesionales deben respetar el límite semanal configurado y rechazar el exceso con una respuesta de rate limit.
* **Médicos:** no deben depender de la study list nativa de OHIF como workflow principal. Deben usar un panel propio del portal con búsqueda, estado federado y retrieve.

## 6.1 Superficies de UI futuras

### 6.1.1 Superficie Paciente
* Lista propia del portal con estudios autorizados del paciente.
* Posibles filtros simples orientados a experiencia de paciente:
  * fecha;
  * modalidad;
  * descripción resumida;
  * estado de disponibilidad.
* Acción principal: abrir un estudio puntual en Stone Web Viewer, manteniendo OHIF como alternativa explícita.
* La experiencia debe ser responsive y usable en móvil.
* El contrato explícito de esta superficie queda definido en `artifacts/05_ui_contracts.md`.
* En el mock actual del portal, el ingreso de paciente debe aterrizar primero en esta superficie y no redirigir directamente a la home general de OHIF.
* La primera implementación funcional de esta superficie consume `GET /api/patient/studies?document=<dni>` y renderiza la lista desde datos del backend.
* La búsqueda remota de estudios del paciente debe ejecutarse mediante workers Go en background, persistiendo estado en `search_requests` y `search_node_runs`, mientras la UI sigue mostrando la lista cacheada y un indicador de búsqueda en curso.
* La UI paciente debe reutilizar la búsqueda remota en curso cuando el operador repita el mismo `documento + filtros`, evitando emitir `POST /api/patient/search` redundantes mientras exista un `request_id` activo para esa combinación.
* Si el backend reinicia con búsquedas paciente en `queued` o `running`, esas filas huérfanas deben cerrarse como fallidas; la UI no debe heredar un estado `Buscando...` indefinido desde ejecución previa.
* Los estudios remotos de esta lista deben quedar inicialmente como `pending_retrieve`, con botón explícito `Retrieve` para traerlos a Orthanc antes de habilitar `Visualizar estudio`.
* Cuando un retrieve de paciente llegue a estado terminal, la lista debe refrescarse de manera silenciosa: sin vaciar la grilla, sin mostrar placeholders intermedios y preservando scroll/foco para evitar parpadeo o pérdida de contexto.

### 6.1.2 Superficie Médico
* Panel propio del portal para búsqueda federada y operación.
* Debe incluir, como mínimo:
  * filtros de búsqueda;
  * lista de resultados;
  * origen o nodos PACS remotos;
  * disponibilidad local en caché;
  * estado de retrieve;
  * acción de retrieve;
  * acción de visualización puntual en Stone Web Viewer;
  * acción alternativa explícita `Visualizar con OHIF Viewer`.
* La experiencia debe ser responsive y usable en móvil, al menos para consulta y validación rápida.
* El contrato explícito de esta superficie queda definido en `artifacts/05_ui_contracts.md`.
* En el mock actual del portal, el ingreso profesional debe aterrizar primero en esta superficie y no redirigir directamente a la home general de OHIF.
* La primera implementación funcional de esta superficie consume `GET /api/physician/results?username=<dni>` y, sin filtros, debe mostrar siempre los estudios locales en cache consultando Orthanc local en vivo para la ventana relativa definida por `professional.initial_cache_period`.
* El primer avance operativo de esta superficie expone `POST /api/physician/retrieve`, reutiliza Orthanc REST para `C-GET` y recalcula `cache_status` / `retrieve_status` desde Postgres y Orthanc local antes de habilitar las acciones de visualización.
* Cuando un retrieve profesional llega a estado terminal, la grilla debe refrescarse de manera silenciosa: sin vaciar la lista, sin mostrar placeholders intermedios y preservando scroll/foco para evitar parpadeo o pérdida de contexto.
* Tanto en paciente como en profesional, si el PACS origen del estudio está offline según la salud actual del sistema, la acción `Recuperar estudio` debe quedar deshabilitada con la leyenda `Origen no disponible`, y el backend debe rechazar igualmente el enqueue si recibe el POST.
* Con filtros cargados, `GET /api/physician/results` debe consultar QIDO-RS del nodo remoto configurado y persistir esa búsqueda como reciente para reutilización posterior.
* El filtro `patient_name` del profesional debe comportarse como búsqueda fuzzy por términos normalizados, no como coincidencia literal exacta.

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
* Nginx publica el portal y el visor sobre `http://localhost:8081`.
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
      "protocol": "hybrid",
      "priority": 1,
      "search": {
        "mode": "qido_rs",
        "dicomweb_base_url": "https://pending-host/dcm4chee-arc/aets/CENTRAL_PACS/rs"
      },
      "retrieve": {
        "mode": "c_move",
        "aet": "CENTRAL_PACS",
        "dicom_host": "pending-host",
        "dicom_port": 11112,
        "supports_cmove": true,
        "supports_cget": false
      },
      "health": {
        "mode": "mixed"
      }
    },
    {
      "id": "sede_norte",
      "name": "Orthanc Norte",
      "protocol": "dicomweb",
      "priority": 2,
      "search": {
        "mode": "qido_rs",
        "dicomweb_base_url": "https://pending-host/orthanc/dicom-web"
      },
      "health": {
        "mode": "http"
      }
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
* **TO-DO de seguridad real:** la expiración actual en la UI principal es una medida de experiencia y no reemplaza sesiones backend reales; queda pendiente implementar sesiones de servidor para paciente y profesional, expiración validada del lado backend y control efectivo de acceso a Stone/OHIF/DICOMweb por sesión activa + `StudyInstanceUID` autorizado.

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
