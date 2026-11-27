# Using the migrate package with pgxpool

`Migrator` needs a `*pgx.Conn`. Acquire a connection from the pool and keep it
until the migrator has finished. Release it before closing the pool. The
following program applies the SQL files in `./migrations`:

```go
package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"
)

func migrateDatabase(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	migrator, err := migrate.NewMigrator(ctx, conn.Conn(), "public.schema_version")
	if err != nil {
		return err
	}
	if err := migrator.LoadMigrations(migrations); err != nil {
		return err
	}
	return migrator.Migrate(ctx)
}

func run(ctx context.Context) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("set DATABASE_URL to a PostgreSQL connection string")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	return migrateDatabase(ctx, pool, os.DirFS("migrations"))
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
```

For a disposable test database, save the program as `main.go` in a Go module, set
`DATABASE_URL` to a PostgreSQL connection string, and create a migration such
as `migrations/001_create_widgets.sql`:

```sql
create table widgets (id bigint primary key);

---- create above / drop below ----

drop table widgets;
```

Run `go mod tidy` to add the imports, then `go run .`. Tern creates the version
table and applies the migration. See [Migrations](../README.markdown#migrations)
for the file format and ordering rules.
