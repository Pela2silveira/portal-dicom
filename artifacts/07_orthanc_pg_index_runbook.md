# Runbook — Migrar el índice de Orthanc de SQLite a PostgreSQL

> **Objetivo:** eliminar la saturación de CPU/disco y los timeouts de `POST /tools/find`
> causados por el índice **SQLite de un solo escritor** de Orthanc, moviéndolo a
> **PostgreSQL**. Solo se migra el **índice/metadata**; los archivos DICOM siguen en el
> **storage area en filesystem** (`./volumes/orthanc-storage`).

## 1) Contexto y diagnóstico

- Orthanc corre hoy con el índice **SQLite** (`/system` → `"DatabaseBackendPlugin": null`,
  `ORTHANC__POSTGRESQL__ENABLEINDEX=false`, log `SQLite index directory: /var/lib/orthanc/db`).
- El archivo `index` supera los ~500 MB; bajo consultas por fecha concurrentes (`/tools/find`)
  y la carga de metadata de OHIF, el motor SQLite (un solo escritor, contención de WAL)
  satura CPU/disco y llega a timeoutear el listado del profesional (`physician_results_query_failed`,
  `context deadline exceeded`).
- PostgreSQL maneja lecturas/escrituras concurrentes mucho mejor, por lo que resuelve la
  contención de raíz. El cableado ya existe en `app/docker-compose.yml`
  (`ORTHANC__POSTGRESQL__*`); solo falta activarlo con `ORTHANC_PG_INDEX_ENABLED=true` y
  crear la base de datos.

## 2) Realidad importante: NO hay migración automática

Cambiar el backend del índice **no migra los datos existentes**. Al activar el índice PG con
una base vacía, Orthanc arranca con un **índice vacío**: no verá los estudios ya cacheados
aunque sus archivos sigan en `orthanc-storage`. Esto está documentado por Orthanc (el cambio
de configuración no mueve ni el índice ni los archivos). Hay que elegir explícitamente una de
las dos estrategias de abajo.

Como decisión de diseño del proyecto, **Orthanc es una caché reconstruible** (los estudios se
re-recuperan on-demand vía retrieve), por lo que la **Opción A** suele ser la adecuada.

## 3) Prerrequisitos y backup (obligatorio)

En el host del stack (`/opt/andes/portalimagenes/app`):

```bash
# Ventana de mantenimiento: el portal no podrá listar/visualizar la caché mientras dure.
# Backup del índice SQLite actual y del storage (por si hay que volver atrás).
mkdir -p backups/$(date +%F)
docker cp app-orthanc:/var/lib/orthanc/db backups/$(date +%F)/orthanc-db-sqlite
# (El storage es grande; basta con NO borrarlo hasta validar. No hace falta copiarlo si hay espacio.)
```

## 4) Opción A — Índice PG limpio (recomendada para caché)

La caché queda vacía y se repuebla on-demand a medida que los usuarios recuperan estudios.
Es la de menor complejidad y downtime.

```bash
# 1. Crear la base de datos del índice (una sola vez).
docker exec app-postgres psql -U portal -d portal -c "CREATE DATABASE orthanc OWNER portal;"

# 2. Activar el índice PG en el .env del stack.
#    Editar /opt/andes/portalimagenes/app/.env y agregar/ajustar:
#    ORTHANC_PG_INDEX_ENABLED=true

# 3. (Recomendado para consistencia de disco) vaciar el índice SQLite y los archivos
#    huérfanos del storage, para que la caché arranque coherente. SOLO si ya hiciste el backup.
docker compose stop orthanc
rm -rf ./volumes/orthanc-db/*        # índice SQLite viejo (ya respaldado)
rm -rf ./volumes/orthanc-storage/*   # archivos que quedarían huérfanos sin índice

# 4. Levantar Orthanc con el índice PG.
docker compose up -d orthanc
docker logs -f app-orthanc 2>&1 | grep -iE "postgres|database version|index directory"
```

Si preferís **no** borrar el storage en el paso 3, podés dejar los archivos; quedarán
huérfanos (ocupan disco sin estar indexados) y Orthanc los irá reciclando según
`MaximumStorageMode`. Preferible borrarlos para partir limpio.

## 5) Opción B — Preservar los estudios ya cacheados

Solo si necesitás conservar la caché actual indexada. No es transparente; requiere copiar los
datos a la instancia con índice PG. Métodos soportados por Orthanc:

- **`Replicate.py`** (recomendado por Orthanc): copia vía REST desde el Orthanc SQLite hacia un
  Orthanc con índice PG. Requiere tener ambas instancias accesibles durante la copia.
- **Advanced Storage plugin + Indexer con `TakeOwnership`**: adopta los archivos existentes del
  storage bajo el nuevo índice PG (ver el sample oficial `orthanc-setup-samples/docker/sqlite-to-postgresql`).

Ambos caminos son más largos y deben planificarse aparte; para una caché reconstruible no
suelen justificarse frente a la Opción A.

## 6) Verificación post-migración

```bash
# Backend del índice = PostgreSQL
curl -s http://127.0.0.1:8042/system | python3 -m json.tool | grep -i DatabaseBackendPlugin
# esperado: "DatabaseBackendPlugin": "PostgreSQL"

# La base tiene el esquema de Orthanc
docker exec app-postgres psql -U portal -d orthanc -c "\dt" | head

# /tools/find responde rápido y ya no timeoutea; observar CPU/disco de app-orthanc
docker stats --no-stream app-orthanc
```

- Confirmar que una búsqueda `local_cache` del profesional deja de devolver `500`
  (`physician_results_query_failed` desaparece del log del backend).
- El volumen `postgres-data` empieza a crecer con la escritura del índice; el `index-wal` de
  SQLite deja de existir/crecer.

## 7) Rollback

```bash
# 1. Volver a SQLite.
#    En .env: ORTHANC_PG_INDEX_ENABLED=false
docker compose stop orthanc

# 2. Restaurar el índice SQLite respaldado (si se había vaciado en la Opción A).
rm -rf ./volumes/orthanc-db/*
docker cp backups/<FECHA>/orthanc-db-sqlite/. app-orthanc:/var/lib/orthanc/db  # o copiar al volumen host
# (Si también se borró el storage, la caché SQLite restaurada apuntará a archivos ausentes;
#  en ese caso conviene quedarse en PG y repoblar on-demand.)

docker compose up -d orthanc
curl -s http://127.0.0.1:8042/system | python3 -m json.tool | grep -i DatabaseBackendPlugin
# esperado tras rollback: "DatabaseBackendPlugin": null
```

## 8) Notas

- Solo el **índice** se mueve a PostgreSQL. El **storage area** (archivos DICOM) sigue en
  filesystem (`ENABLESTORAGE` no se activa; `StorageAreaPlugin` queda en `null`).
- La base `orthanc` comparte instancia de Postgres con `portal` (backend); es una DB separada,
  no interfiere con las tablas operativas del backend.
- Downtime: durante los pasos 3–4 el portal no puede listar/visualizar la caché.
- Mitigación complementaria (fuera de este runbook): revertir `enableStudyLazyLoad` a `true`
  en `app/ohif/app-config.js` para reducir la amplificación de lecturas de OHIF al abrir estudios.
