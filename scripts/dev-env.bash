# This file is sourced by mise. Keep it read-only and Bash 3.2 compatible.

tern_repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"

if [ -z "${TERN_DEV_STATE_DIR:-}" ]; then
  TERN_DEV_STATE_DIR="$tern_repo_root/.dev"
elif [ "${TERN_DEV_STATE_DIR#/}" = "$TERN_DEV_STATE_DIR" ]; then
  TERN_DEV_STATE_DIR="$tern_repo_root/$TERN_DEV_STATE_DIR"
fi

tern_pg_bindir=""
if command -v postgres >/dev/null 2>&1; then
  tern_postgres_path="$(command -v postgres)"
  case "$("$tern_postgres_path" --version 2>/dev/null || true)" in
    *"PostgreSQL) 18."*) tern_pg_bindir="$(dirname "$tern_postgres_path")" ;;
  esac
fi
if [ -z "$tern_pg_bindir" ] && command -v pg_config >/dev/null 2>&1; then
  tern_pg_config_bindir="$(pg_config --bindir 2>/dev/null || true)"
  if [ -x "$tern_pg_config_bindir/postgres" ]; then
    case "$("$tern_pg_config_bindir/postgres" --version 2>/dev/null || true)" in
      *"PostgreSQL) 18."*) tern_pg_bindir="$tern_pg_config_bindir" ;;
    esac
  fi
fi
if [ -z "$tern_pg_bindir" ] && command -v brew >/dev/null 2>&1; then
  tern_pg_prefix="$(brew --prefix postgresql@18 2>/dev/null || true)"
  if [ -n "$tern_pg_prefix" ]; then
    tern_pg_bindir="$tern_pg_prefix/bin"
  fi
  unset tern_pg_prefix
fi

if [ -n "$tern_pg_bindir" ] && [ -d "$tern_pg_bindir" ]; then
  export PATH="$tern_pg_bindir:$PATH"
fi

export TERN_DEV_STATE_DIR
export PGDATA="$TERN_DEV_STATE_DIR/postgres/data"
export TERN_PG_SOCKET_DIR="$TERN_DEV_STATE_DIR/postgres/socket"
export PGHOST="127.0.0.1"
export PGUSER="postgres"
export PGDATABASE="postgres"
export PGSSLMODE="disable"
export TERN_TEST_DATABASE="tern_test"
export MIGRATE_TEST_DATABASE="tern_migrate_test"
export TERN_TEST_CONFIG="$TERN_DEV_STATE_DIR/tern.conf"
export PC_CONFIG_FILES="$tern_repo_root/process-compose.yaml"
export PC_LOG_FILE="$TERN_DEV_STATE_DIR/process-compose.log"
export PC_DISABLE_DOTENV="1"
export TERN_PC_CONFIG_HOME="$TERN_DEV_STATE_DIR/process-compose-config"

if command -v shasum >/dev/null 2>&1; then
  tern_state_checksum="$(printf '%s' "$TERN_DEV_STATE_DIR" | shasum -a 256 | cut -c1-12)"
else
  tern_state_checksum="$(printf '%s' "$TERN_DEV_STATE_DIR" | cksum | awk '{print $1}')"
fi
export PC_SOCKET_PATH="/tmp/tern-pc-$tern_state_checksum.sock"

if [ -r "$TERN_DEV_STATE_DIR/port" ]; then
  tern_saved_port="$(sed -n '1p' "$TERN_DEV_STATE_DIR/port")"
  case "$tern_saved_port" in
    ''|*[!0-9]*) ;;
    *)
      export PGPORT="$tern_saved_port"
      export TERN_TEST_CONN_STRING="host=$PGHOST port=$PGPORT user=$PGUSER database=$TERN_TEST_DATABASE sslmode=$PGSSLMODE"
      export MIGRATE_TEST_CONN_STRING="host=$PGHOST port=$PGPORT user=$PGUSER database=$MIGRATE_TEST_DATABASE sslmode=$PGSSLMODE"
      ;;
  esac
  unset tern_saved_port
fi

unset tern_repo_root tern_pg_bindir tern_pg_config_bindir tern_postgres_path tern_state_checksum
