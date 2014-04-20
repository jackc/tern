# Tern - The SQL Fan's Migrator

Tern is a standalone migration tool for PostgreSQL.

# Installation

    go install github.com/JackC/tern

# Creating a Tern Project

Tern projects are composed of a config JSON file and a directory of
migrations. See the sample directory for an example.By default tern will look
in the current directory for the config file tern.json and the migrations.

The config JSON file requires socket or host, database, and user. port,
password, and versionTable are optional.

```json
{
  "socket": "/var/run/postgresql/.s.PGSQL.5432",
  "database": "tern_test",
  "user": "jack"
}
```

The migrations themselves have an extremely simple file format. They are simply the up and down SQL statements divided by a magic comment.

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

Migrations are read from files in the migration directory in lexicographic
order. Typically, migration files will be prefix with 001, 002, 003, etc., but
that is not strictly necessary, Each migration is run in a transaction.

# Migrating

To migrate up to the last version using migrations and config file located in
the same directory simply run tern:

    tern

To migrate up or down to a specific version:

    tern --destination 42

To use a different config file:

    tern --config path/to/tern.json

# Embedding Tern

All the actual functionality of tern is in the github.com/JackC/tern/migrate
library. If you need to embed migrations into your own application this
library can help.

# Prior Ruby Gem Version

The projects using the prior version of tern that was distributed as a Ruby
Gem are incompatible with the version 1 release. However, that version of tern
is still available through RubyGems and the source code is on the ruby branch.


# Version History

* **1.0.0**
  * Total rewrite in Go
* **0.7.1**
  * Print friendly error message when database error occurs instead of stack trace.
* **0.7.0**
  * Added ERB processing to SQL files

# License

Copyright (c) 2011-2014 Jack Christensen, released under the MIT license
