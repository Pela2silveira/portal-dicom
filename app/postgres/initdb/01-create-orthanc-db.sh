#!/bin/sh
# Creates the separate "orthanc" database used by the Orthanc PostgreSQL index
# plugin (see orthanc/orthanc.json -> PostgreSQL.Database). The official
# postgres image only auto-creates POSTGRES_DB (the app database), so without
# this the Orthanc container loops on: FATAL database "orthanc" does not exist.
# Runs only on first initialization (empty data dir). Idempotent via \gexec.
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
	SELECT 'CREATE DATABASE orthanc OWNER "$POSTGRES_USER"'
	WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'orthanc')\gexec
EOSQL
