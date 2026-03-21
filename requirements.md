# Especificación Técnica: Portal DICOM Agregador y Caché

## 1. Visión General del Sistema
Este sistema es un portal web de imágenes médicas diseñado para centralizar el acceso a múltiples nodos PACS remotos (dcm4chee, Orthanc, Legacy). Utiliza un **PACS local de paso (Caché)** para alimentar al visor **OHIF**, garantizando una visualización rápida y desacoplada de la latencia de las redes remotas.

---

## 2. Alcance del MVP Inicial

El primer entregable debe enfocarse en una base operativa mínima, sin autenticación de usuarios finales.

### 2.1 En alcance para el MVP
* **Infraestructura con Docker Compose:** levantar todos los servicios base del sistema localmente.
* **PACS local de caché:** Orthanc local, preparado para recibir estudios desde PACS remotos.
* **Backend en Go:** servicio agregador para consultar nodos remotos, gestionar retrieves y exponer endpoints internos del portal.
* **Base de datos de aplicación:** PostgreSQL para almacenar configuración operativa, jobs de retrieve, auditoría técnica y metadatos locales.
* **Nginx:** servir contenido estático y actuar como reverse proxy para backend y visor.
* **Integración con HIS:** el sistema debe permitir configurar credenciales, API keys, URLs base y parámetros necesarios para futuras consultas al HIS.
* **Configuración de PACS remotos:** el sistema debe permitir cargar detalles de conexión para nodos dcm4chee remotos.
* **Visualización desacoplada:** OHIF debe consumir estudios desde el Orthanc local y no desde los PACS remotos.

### 2.2 Fuera de alcance en este MVP
* Login de médicos.
* Login de pacientes.
* OTP por SMS o Email.
* Gestión de sesiones.
* JWT para restricción de acceso a OHIF.
* Permisos finos por usuario.
* Share links o links temporales.

---

## 3. Modelos de Acceso y Autenticación

### 3.1 Fase posterior: Login de Médicos
* **Alcance:** Acceso total a la base de datos de todos los PACS conectados.
* **Búsqueda:** Manual mediante filtros (Rango de fechas, Modalidad, Nombre, DNI).
* **Acción:** Selección de estudios para visualización bajo demanda.

### 3.2 Fase posterior: Login de Pacientes
* **Acceso Seguro:** Validación de DNI + OTP (One-Time Password) vía SMS/Email.
* **Identidad (HIS Integration):** El sistema consulta un servicio REST del HIS para obtener todos los identificadores (ID local, ID nacional, etc.) asociados al DNI del paciente.
* **Búsqueda Implícita:** Al iniciar sesión, se dispara automáticamente la búsqueda en todos los PACS utilizando el array de IDs obtenidos.

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

### 5.2 PACS Local (Caché)
* **Tecnología:** Orthanc (Ligero, API REST potente).
* **Retención:** Política de purga automática de **7 días**. 
* **Configuración:** Debe actuar como `Move SCP` para recibir estudios de los nodos remotos.

---

## 6. Visualización (OHIF Viewer)

* **Integración:** Una vez que el estudio está en el PACS local, el botón "Visualizar" abre OHIF.
* **Configuración del Visor:** OHIF consume datos únicamente del PACS local mediante protocolos DICOMweb (`WADO-RS`).
* **Aislamiento:** El visor no conoce la existencia de los PACS remotos, solo interactúa con el caché.

---

## 7. Infraestructura Base del MVP

### 7.1 Servicios mínimos en Docker Compose
El entorno de desarrollo del MVP debe incluir:
* `orthanc`: PACS local de caché.
* `backend`: servicio Go agregador y coordinador.
* `postgres`: persistencia operativa del sistema.
* `nginx`: reverse proxy y servicio de contenido estático.
* `ohif`: visor conectado al Orthanc local.

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
* parámetros del Orthanc local;
* settings de Nginx para exponer backend, visor y estáticos.

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
* La exposición pública del stack debe pasar por Nginx.

### 9.2 Fase posterior
* Registro de cada DNI consultado, por qué usuario (Médico/Paciente) y desde qué IP.
* El acceso a OHIF debe estar vinculado a la sesión activa del portal.
* Implementación de JWT en el proxy de imágenes para restringir el acceso a nivel de StudyInstanceUID en OHIF.
