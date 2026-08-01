#!/bin/bash
set -e

mise trust
mise install
eval "$(mise env -s bash)"

# The db service starts alongside the app, so it may not be accepting connections
# yet. Connection settings come from the PG* environment in docker-compose.yml.
echo "Waiting for PostgreSQL..."
for _ in $(seq 1 60); do
  if pg_isready -q; then break; fi
  sleep 1
done
pg_isready

# The test suites require these databases to exist. Creation is idempotent: the
# data directory lives on a persistent volume, so on a container rebuild the
# databases are already there. migrate's tests drop and recreate
# tern_migrate_test themselves, but it must exist for the first dropdb to work.
for db in "$TERN_TEST_DATABASE" "$MIGRATE_TEST_DATABASE"; do
  if [ -z "$(psql -tAc "select 1 from pg_database where datname = '$db'")" ]; then
    createdb "$db"
    echo "created database $db"
  fi
done

# tern.conf is gitignored (it holds per-developer connection settings), but
# tern_test.go passes it to the tern binary. The checked-in example already
# points at this devcontainer's socket and database.
if [ ! -e testdata/tern.conf ]; then
  cp testdata/tern.conf.example testdata/tern.conf
fi

# Run any additional setup scripts included in the shared/devcontainer directory. This is to allow for per developer or
# per-environment customizations. These scripts are not checked into source control.
if [ -x "/persist/shared/devcontainer/install" ]; then
  /persist/shared/devcontainer/install
fi

# Create a symlink to the shared .scratch directory for temporary files if it exists.
if [ -x "/persist/shared/.scratch" ]; then
  if [ ! -e .scratch ] && [ ! -L .scratch ]; then
    ln -s /persist/shared/.scratch
  fi
fi
