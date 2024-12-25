# Tern - The SQL Fan's Migrator

Tern is a standalone migration tool for PostgreSQL. It includes traditional migrations as well as a separate optional
workflow for managing database code such as functions and views.

## Features

* Multi-platform
* Stand-alone binary
* SSH tunnel support built-in
* Data variable interpolation into migrations

## Installation

```
go install github.com/jackc/tern/v2@latest
```

## Creating a Tern Project

To create a new tern project in the current directory run:

    tern init

Or to create the project somewhere else:

    tern init path/to/project

Tern projects are composed of a directory of migrations and optionally a
config file. See the sample directory for an example.

# Configuration

Database connection settings can be specified via the standard PostgreSQL
environment variables, via program arguments, or in a config file. By
default tern will look in the current directory for the config file tern.conf
and the migrations.

The `tern.conf` file is stored in the `ini` format with two sections,
`database` and `data`. The `database` section contains settings for connection
to the database server.

Values in the `data` section will be available for interpolation into
migrations. This can help in scenarios where migrations are managing
permissions and the user to which permissions are granted should be
configurable.

If all database settings are supplied by PG* environment variables or program
arguments the config file is not required. In particular, using the `PGSERVICE`
can reduce or eliminate the need for a configuration file.

The entire `tern.conf` file is processed through the Go standard
`text/template` package. [Sprig](http://masterminds.github.io/sprig/) functions
are available.

Example `tern.conf`:

```ini
[database]
# host supports TCP addresses and Unix domain sockets
# host = /private/tmp
host = 127.0.0.1
# port = 5432
database = tern_test
user = jack
password = {{env "MIGRATOR_PASSWORD"}}
# version_table = public.schema_version
#
# sslmode generally matches the behavior described in:
# http://www.postgresql.org/docs/9.4/static/libpq-ssl.html#LIBPQ-SSL-PROTECTION
#
# There are only two modes that most users should use:
# prefer - on trusted networks where security is not required
# verify-full - require SSL connection
# sslmode = prefer
#
# "conn_string" accepts two formats; URI or DSN as described in:
# https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING
#
# This property is lenient i.e., it does not throw error
# if values for both "conn_string" and "host/port/.." are
# provided. In this case, the individual properties will
# override the correspoding part in the "conn_string".
#
# URI format:
# conn_string = postgresql://other@localhost/otherdb?connect_timeout=10&application_name=myapp
# DSN format:
# conn_string = host=localhost port=5432 dbname=mydb connect_timeout=10

# Proxy the above database connection via SSH
# [ssh-tunnel]
# host =
# port = 22
# user defaults to OS user
# user =
# password is not required if using SSH agent authentication
# password =
# keyfile is the path to a SSH key file
# keyfile =
# passphrase for the SSH key file given above or one of the default SSH key files in ~/.ssh
# passphrase =

[data]
prefix = foo
app_user = joe

```

This flexibility configuration style allows handling multiple environments such
as test, development, and production in several ways.

* Separate config file for each environment
* Environment variables for database settings and optionally one config file
  for shared settings
* Program arguments for database settings and optionally one config file for
  shared settings

In addition to program arguments, `TERN_CONFIG` and `TERN_MIGRATIONS`
environment variables may be used to set the config path and migrations path
respectively.

## Migrations

To create a new migration:

    tern new name_of_migration

This will create a migration file with the given name prefixed by the next available sequence number (e.g. 001, 002, 003). The `-e` flag can be used to automatically open the new file in `EDITOR`.

The migrations themselves have an extremely simple file format. They are
simply the up and down SQL statements divided by a magic comment.

    ---- create above / drop below ----

Example:

```sql
create table t1(
  id serial primary key
);

---- create above / drop below ----

drop table t1;
```

If a migration is irreversible such as a drop table, simply delete the magic
comment.

```sql
drop table widgets;
```

To interpolate a custom data value from the config file prefix the name with a
dot and surround the whole with double curly braces.

```sql
create table {{.prefix}}config(
  id serial primary key
);
```

Migrations are read from files in the migration directory in the order of the
numerical prefix. Each migration is run in a transaction.

Any SQL files in subdirectories of the migration directory, will be available
for inclusion with the template command. This can be especially useful for
definitions of views and functions that may have to be dropped and recreated
when the underlying table(s) change.

```sql
// Include the file shared/v1_001.sql. Note the trailing dot.
// It is necessary if the shared file needs access to custom data values.
{{ template "shared/v1_001.sql" . }}
);
```

Tern uses the standard Go
[text/template](http://golang.org/pkg/text/template/) package so conditionals
and other advanced templating features are available if needed. See the
package docs for details. [Sprig](http://masterminds.github.io/sprig/)
functions are also available.

Migrations are wrapped in a transaction by default. Some SQL statements such as `create index concurrently` cannot be performed within a transaction. To disable the transaction include the magic comment:

```
---- tern: disable-tx ----
```

## Migrating

To migrate up to the last version using migrations and config file located in
the same directory simply run tern:

    tern migrate

To migrate up or down to a specific version:

    tern migrate --destination 42

To migrate up N versions:

    tern migrate --destination +3

To migrate down N versions:

    tern migrate --destination -3

To migrate down and rerun the previous N versions:

    tern migrate --destination -+3

To use a different config file:

    tern migrate --config path/to/tern.json

To use a different migrations directory:

    tern migrate --migrations path/to/migrations

## Renumbering Conflicting Migrations

When migrations are created on multiple branches the migrations need to be renumbered when the branches are merged. The
`tern renumber` command can automatically do this. On the branch with the only migrations to keep at the lower numbers
run `tern renumber start`. Merge the branches. Then run `tern renumber finish`.

```
$ git switch master
Switched to branch 'master'
$ ls
001_create_users.sql
002_add_last_login_to_users.sql

$ git switch feature
Switched to branch 'feature'
$ ls
001_create_users.sql
002_create_todos.sql

# Both branches have a migration number 2.

# Run tern renumber start on the branch with the migrations that should come first.

$ git switch master
Switched to branch 'master'
$ tern renumber start

# Then go to the branch with migrations that should come later and merge or rebase.

$ git switch feature
$ git rebase master
Successfully rebased and updated refs/heads/feature.
$ ls
001_create_users.sql
002_add_last_login_to_users.sql
002_create_todos.sql

# There are now two migrations with the same migration number.

$ tern renumber finish
$ ls
001_create_users.sql
002_add_last_login_to_users.sql
003_create_todos.sql

# The migrations are now renumbered in the correct order.
```

## Code Packages

The migration paradigm works well for creating and altering tables, but it can be unwieldy when dealing with database
code such as server side functions and views. For example, consider a schema where view `c` depends on view `b` which
depends on view `a`. A change to `a` may require the following steps:

1. Drop `c`
2. Drop `b`
3. Drop `a`
4. Create `a`
5. Create `b`
6. Create `c`

In addition to the challenge of manually building such a migration it is difficult to use version control to see the
changes in a particular database object over time when its definition is scattered through multiple migrations.

A solution to this is code packages. A code package is a directory with an `install.sql` file that contains the
instructions to completely drop and recreate a set of database code. The command `code install` can be used to directly
install a code package (especially useful during development) and the `code snapshot` command can be used to make a
single migration that installs that code package.

For example given a directory `code` containing the following files:

```
-- install.sql
drop schema if exists code cascade;
create schema code;

{{ template "a.sql" . }}
{{ template "b.sql" . }}
{{ template "c.sql" . }}
```

```
-- a.sql
create view code.a as select ...;
```

```
-- b.sql
create view code.b as select * from code.a where ...;
```

```
-- c.sql
create view code.c as select * from code.b where ...;
```

Then this command would install the package into the database.

```
tern code install path/to/code --config path/to/tern.conf
```

And this command would create a migration from the current state of the code package.

```
tern code snapshot path/to/code --migrations path/to/migrations
```

Code packages have access to data variables defined in your configuration file as well as functions provided by
[Sprig](http://masterminds.github.io/sprig/).

It is recommended but not required for each code package to be installed into its own PostgreSQL schema. This schema
could be determined by environment variable as part of a blue / green deployment process.

## Template Tips

The `env` function can be used to read process environment variables.

```
drop schema if exists {{ env "CODE_SCHEMA" }} cascade;
create schema {{ env "CODE_SCHEMA" }};
```

The [Sprig dictionary functions](http://masterminds.github.io/sprig/dicts.html) can be useful to call templates with extra parameters merged into the `.` value.

```
{{ template "_view_partial.sql" (merge (dict "view_name" "some_name" "where_clause" "some_extra_condition=true") . ) }}
```

## SSH Tunnel

Tern includes SSH tunnel support. Simply supply the SSH host, and optionally
port, user, and password in the config file or as program arguments and Tern
will tunnel the database connection through that server. When using a SSH tunnel
the database host should be from the context of the SSH server. For example, if
your PostgreSQL server is `pg.example.com`, but you only have SSH access, then
your SSH host would be pg.example.com and your database host would be
`localhost`.

Tern will automatically use an SSH agent or `~/.ssh/id_dsa`, `~/.ssh/id_rsa`,
`~/.ssh/ed25519` and`~/.ssh/id_ecdsa` if available.

## Embedding Tern

All the actual functionality of tern is in the github.com/jackc/tern/v2/migrate
library. If you need to embed migrations into your own application this
library can help. If you don't need the full functionality of tern, then a migration generator script as described below may be a easier way of embedding simple migrations.

## Generating a Migration Generator SQL Script

Sometimes an application or plugin needs to perform migrations but it is not the owner of the database and tern is not
available.

The `gengen` command generates a SQL script that when run against a database will generate a SQL script with the
migrations necessary to bring the database to the latest schema version.

For example, a Go job queue library may need to perform database migrations, but it does not want to require its users
to also use tern. In this case the plugin author would use `gengen` to create a SQL script that inspects that database
and creates the actual SQL migration script. This script can then be integrated with whatever schema management system
the host application is using.

Usage:

```
$ tern gengen > generate-migrations.sql
$ psql --no-psqlrc --tuples-only --quiet --no-align -f generate-migrations.sql mydb > migrations.sql

# migrations.sql now contains the commands to migrate mydb to the latest schema version. It can be run directly or
# integrated with another migrate system.
```

Limitations:

* Every migration runs in a transaction. That is, if a migration has a
disable-tx magic comment it will be ignored.
* Migrations can only go forward to the latest version.

## Running the Tests

To run the tests tern requires two test databases to run migrations against.

1. Create a new database for main tern program tests (e.g. `tern_test`).
2. Open testdata/tern.conf.example
3. Enter the connection information.
4. Save as testdata/tern.conf.
5. Run tests with the connection string for the main tern program tests in the TERN_TEST_CONN_STRING environment variable.
6. Create another database for the migrate library tests (e.g. `tern_migrate_test`).
7. Run tests with the connection string for the migrate library tests in the MIGRATE_TEST_CONN_STRING environment variable

```
TERN_TEST_CONN_STRING="host=/private/tmp database=tern_test" MIGRATE_TEST_CONN_STRING="host=/private/tmp database=tern_migrate_test" MIGRATE_TEST_DATABASE=tern_migrate_test go test ./...
```

## Prior Ruby Gem Version

The projects using the prior version of tern that was distributed as a Ruby
Gem are incompatible with the version 1 release. However, that version of tern
is still available through RubyGems and the source code is on the ruby branch.

## Version History

## 2.3.2 (December 25, 2024)

* Fix goreleaser build script

## 2.3.1 (December 21, 2024)

* Upgrade vulnerable golang.org/x/crypto library

## 2.3.0 (November 27, 2024)

* Add print-migrations command
* Check more default SSH key locations
* Windows fixes and improved Windows SSH support

## 2.2.3 (October 1, 2024)

* Fix: version command for v2.2.2 was not updated and still outputted v2.2.1.

## 2.2.2 (September 29, 2024)

* Fix: default sslmode=prefer behavior could cause fallback config confusion that could cause some connection settings to be ignored when not over ssl.
* Update sprig dependency
* Add docker images to goreleaser (odyfey)

## 2.2.1 (June 13, 2024)

* Fix: handle dollar-quoted strings when splitting SQL statements (Krzysztof Szafrański)
* Fix: code snapshot command uses TERN_MIGRATIONS environment variable
* Fix: code snapshot command sets file permissions properly

## 2.2.0 (May 17, 2024)

* Add gengen command
* Upgrade dependencies
* Fixes for SSH tunnel port

## 2.1.1 (June 17, 2023)

* Upgrade to pgx v5.4.0 - This should resolve ssh tunnel issues
* CLI tool sets application_name to "tern" by default
* Fix FindMigrations panic with missing migrations (Billy Keyes)

## 2.1.0 (April 13, 2023)

* Add print-connstring command to CLI (abs3ntdev)
* Allow multiple config files on CLI (abs3ntdev)
* Fix port being ignored in config file if sslmode is set (abs3ntdev)

## 2.0.1 (March 2, 2023)

* Fix -e flag with terminal editors (abs3nt)

## 2.0.0 (February 23, 2023)

* Remove deprecated env access syntax in config file
* Replace MigratorFS interface with fs.FS
* Upgrade to pgx v5
* Upgrade to sprig v3
* Add TERN_CONFIG and TERN_MIGRATIONS environment variables
* Add -e flag to tern new to open file in EDITOR
* Add per migration `disable-tx` option
* Add renumber commands to help when merging branches that have conflicting migration numbers

## 1.13.0 (April 21, 2022)

* Add conn string connection config option (vivek-shrikhande)
* Add Filename to MigrationPgError (davidmdm)

## 1.12.5 (June 12, 2021)

* Look for SSH keys in `~/.ssh/id_rsa` (Miles Delahunty)

## 1.12.4 (February 27, 2021)

* Use user's known_hosts file when connecting via SSH

## 1.12.3 (December 24, 2020)

* Fix reported version number

## 1.12.2 (December 23, 2020)

* Fix setting port from config file
* Fix non-schema qualified version table not in public but in search path (Tynor Fujimoto)

## 1.12.1 (June 27, 2020)

* Update to latest version of pgx.

## 1.12.0 (June 26, 2020)

* Command code install no longer outputs compiled SQL.
* Add code compile command to print compiled SQL code package.
* Better error reporting for code install.

## 1.11.0 (April 10, 2020)

* Add [Sprig](http://masterminds.github.io/sprig/) functions to configuration file and migrations.
* Add SQL code management distinct from migrations.

## 1.10.2 (March 28, 2020)

* CLI now handles SIGINT (ctrl+c) and attempts to cancel in-progress migration before quitting

## 1.10.1 (March 24, 2020)

* Fix default CLI version-table argument overriding config value

## 1.10.0 (March 7, 2020)

* Better locking to protect against multiple concurrent migrators on first run
* Update pgx version to support PostgreSQL service files

## 1.9.1 (February 1, 2020)

* Look for version table in all schemas in search_path instead of just the top schema

## 1.9.0 (February 1, 2020)

* Default version table is explicitly in public schema
* Update to pgx v4 (Alex Gaynor)

## 1.8.2 (July 19, 2019)

* Show PostgreSQL error details
* Rename internal error type

## 1.8.1 (April 5, 2019)

* Issue `reset all` after every migration
* Use go modules instead of Godep / vendoring

## 1.8.0 (February 26, 2018)

* Update to latest version of pgx (PostgreSQL driver)
* Support PGSSLROOTCERT
* Fix typos and internal cleanup
* Refactor internals for easier embedding (hsyed)

## 1.7.1 (January 30, 2016)

* Simplify SSH tunnel code so it does not listen on localhost

## 1.7.0 (January 17, 2016)

* Add SSH tunnel support

## 1.6.1 (January 16, 2016)

* Fix version output
* Evaluate config files through text/template with ENV

## 1.6.0 (January 15, 2016)

* Optionally read database connection settings from environment
* Accept database connection settings via program arguments
* Make config file optional

## 1.5.0 (October 1, 2015)

* Add status command
* Add relative migration destinations
* Add redo migration destinations

## 1.4.0 (May 15, 2015)

* Add TLS support

## 1.3.3 (May 1, 2015)

* Fix version output

## 1.3.2 (May 1, 2015)

* Better error messages

## 1.3.1 (December 24, 2014)

* Fix custom version table name

## 1.3.0 (December 23, 2014)

* Prefer host config whether connecting with unix domain socket or TCP

## 1.2.2 (May 30, 2014)

* Fix new migration short name

## 1.2.1 (May 18, 2014)

* Support socket directory as well as socket file

## 1.2.0 (May 6, 2014)

* Move to subcommand interface
* Require migrations to begin with ascending, gapless numbers
* Fix: migrations directory can contain other files
* Fix: gracefully handle invalid current version
* Fix: gracefully handle migrations with duplicate sequence number

## 1.1.1 (April 22, 2014)

* Do not require user -- default to OS user

## 1.1.0 (April 22, 2014)

* Add sub-template support
* Switch to ini for config files
* Add custom data merging

## 1.0.0 (April 19, 2014)

* Total rewrite in Go

## 0.7.1

* Print friendly error message when database error occurs instead of stack trace.

## 0.7.0

* Added ERB processing to SQL files

## License

Copyright (c) 2011-2014 Jack Christensen, released under the MIT license
