# Tern - The SQL Fan's Migrator

Tern is a standalone migration tool for PostgreSQL.

## Installation

    go get github.com/JackC/tern

## Creating a Tern Project

To create a new tern project in the current directory run:

    tern init

Or to create the project somewhere else:

    tern init path/to/project

Tern projects are composed of a config file and a directory of migrations. See
the sample directory for an example. By default tern will look in the current
directory for the config file tern.conf and the migrations.

The config file requires socket or host and database. User defaults to the
current OS user.

```ini
[database]
socket = /private/tmp/.s.PGSQL.5432
# host = 127.0.0.1
# port = 5432
database = tern_test
user = jack
# password = secret
# version_table = schema_version

[data]
prefix = foo

```

Values in the data section will be available for interpolation into migrations.

## Migrations

To create a new migration:

    tern new name_of_migration

This will create a migration file with the given name prefixed by the next available sequence number (e.g. 001, 002, 003).

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
package docs for details.

## Migrating

To migrate up to the last version using migrations and config file located in
the same directory simply run tern:

    tern migrate

To migrate up or down to a specific version:

    tern migrate --destination 42

To use a different config file:

    tern migrate --config path/to/tern.json

To use a different migrations directory:

    tern migrate --migrations path/to/migrations

## Embedding Tern

All the actual functionality of tern is in the github.com/JackC/tern/migrate
library. If you need to embed migrations into your own application this
library can help.

## Running the Tests

To run the tests tern requires a test database to run migrations against.

1. Create a new database for main tern program tests.
1. Open testdata/tern.conf.example
1. Enter the connection information.
1. Save as testdata/tern.conf.
1. Create another database for the migrate library tests.
1. Open migrate/connection_settings_test.go.example.
1. Enter the second database's connection information.
1. Save as migrate/connection_settings_test.go.

    go test ./...

## Prior Ruby Gem Version

The projects using the prior version of tern that was distributed as a Ruby
Gem are incompatible with the version 1 release. However, that version of tern
is still available through RubyGems and the source code is on the ruby branch.

## Version History

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
