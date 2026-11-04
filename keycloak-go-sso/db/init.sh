#!/bin/sh
# PostgreSQLの初回起動時に一度だけ実行される初期化スクリプト。

set -eu

# keycloak用とapp用にDBとRoleを分け、互いのDBへ接続できないようにする。
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  --set=kc_password="$KC_DB_PASSWORD" --set=app_password="$APP_DB_PASSWORD" <<'SQL'
CREATE ROLE keycloak LOGIN PASSWORD :'kc_password';
CREATE ROLE app LOGIN PASSWORD :'app_password';

CREATE DATABASE keycloak OWNER keycloak;
CREATE DATABASE app OWNER app;

REVOKE ALL ON DATABASE keycloak FROM PUBLIC;
REVOKE ALL ON DATABASE app FROM PUBLIC;
SQL

# documentsテーブルをappロールの所有で作る。owner_subにはKeycloakのsubが入る。
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname app <<'SQL'
SET ROLE app;

CREATE TABLE documents (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  owner_sub text NOT NULL,
  title text NOT NULL,
  body text NOT NULL
);

CREATE INDEX documents_owner ON documents(owner_sub);
SQL
