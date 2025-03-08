package migrate

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/tern/v2/migrate/internal/sqlsplit"
)

// A MigrationStep is a database schema state transition. It performs the modifications needed to
// bring the database schema up from its prior state to the new state (and optionally back down
// again).
type MigrationStep interface {
	// Name is a human-readable name or description of the [Step].
	Name() string
	// Sequence is a state identifier for the database schema after the Up method has been
	// applied for this [Migration].
	Sequence() int32
	// DisableTx indicates if this [Migration] cannot be run inside a transaction. Some SQL
	// statements such as `create index concurrently` cannot run within a transaction. If the
	// return value is true, the [Migrator] won't start a transaction for the [pgx.Conn] passed
	// to [Up] (or [Down]).
	DisableTx() bool
	// Up performs the database operations necessary to bring the database from its prior state
	// to a state that includes the modifications performed by this [Migration]. The [pgx.Conn]
	// may or may not be running in the context of a transaction depending on [DisableTx].
	Up(ctx context.Context, conn *pgx.Conn) error
	// Down performs the database operations necessary to bring the database back to its prior
	// state by undoing the modifications of this [Migration]. The [pgx.Conn] may or may not be
	// running in the context of a transaction depending on [DisableTx].
	//
	// If this [MigrationStep] cannot be undone a [IrreversibleMigrationError] will be returned.
	Down(ctx context.Context, conn *pgx.Conn) error
}

// SQLMigrationStep is a specialization of a [MigrationStep] for a migration that is entirely
// defined with SQL statements (no custom code execution).
type SQLMigrationStep interface {
	MigrationStep

	UpSQL() string
	DownSQL() string
}

// SQL defines a declarative, entirely SQL-based, migration that can be passed to the [SQLStep]
// function to produce an [SQLMigrationStep].
type SQL struct {
	Sequence int32
	Name     string
	UpSQL    string
	DownSQL  string
}

// SQLStep creates a [MigrationStep] that uses declarative SQL statements to define migration
// operations.
func SQLStep(s SQL) *sqlMigrationStep {
	return &sqlMigrationStep{sequence: s.Sequence, name: s.Name, upSQL: s.UpSQL, downSQL: s.DownSQL}
}

var _ MigrationStep = &sqlMigrationStep{}
var _ SQLMigrationStep = &sqlMigrationStep{}

type sqlMigrationStep struct {
	sequence int32
	name     string
	upSQL    string
	downSQL  string
}

func (m *sqlMigrationStep) Name() string { return m.name }

func (m *sqlMigrationStep) Sequence() int32 { return m.sequence }

func (m *sqlMigrationStep) UpSQL() string { return m.upSQL }

func (m *sqlMigrationStep) DownSQL() string { return m.downSQL }

func (m *sqlMigrationStep) DisableTx() bool { return disableTxPattern.MatchString(m.upSQL) }

func (m *sqlMigrationStep) Up(ctx context.Context, conn *pgx.Conn) error {
	return m.connExec(ctx, conn, m.statements(m.upSQL))
}

func (m *sqlMigrationStep) Down(ctx context.Context, conn *pgx.Conn) error {
	if m.downSQL == "" {
		return IrreversibleMigrationError{m: m}
	}
	return m.connExec(ctx, conn, m.statements(m.downSQL))
}

func (m *sqlMigrationStep) connExec(ctx context.Context, conn *pgx.Conn, sqlStatements []string) error {
	for _, statement := range sqlStatements {
		if _, err := conn.Exec(ctx, statement); err != nil {
			if err, ok := err.(*pgconn.PgError); ok {
				return MigrationPgError{MigrationName: m.name, Sql: statement, PgError: err}
			}
			return err
		}
	}
	return nil
}

func (m *sqlMigrationStep) statements(sql string) []string {
	sql = disableTxPattern.ReplaceAllLiteralString(sql, "")
	if m.DisableTx() {
		return sqlsplit.Split(sql)
	}
	return []string{sql}
}

// TxFunc defines a function/code-based migration that can be passed to the [FuncStep] function to
// produce a [MigrationStep].
type TxFunc struct {
	Sequence int32
	Name     string
	Up       func(context.Context, *pgx.Conn) error
	Down     func(context.Context, *pgx.Conn) error
}

// FuncStep creates a [MigrationStep] that uses code (functions) to perform migration operations.
func FuncStep(fn TxFunc) *txFuncMigrationStep {
	return &txFuncMigrationStep{txFunc: fn}
}

var _ MigrationStep = &txFuncMigrationStep{}

type txFuncMigrationStep struct{ txFunc TxFunc }

func (m *txFuncMigrationStep) Name() string { return m.txFunc.Name }

func (m *txFuncMigrationStep) Sequence() int32 { return m.txFunc.Sequence }

func (m *txFuncMigrationStep) DisableTx() bool { return false }

func (m *txFuncMigrationStep) Up(ctx context.Context, conn *pgx.Conn) error {
	return m.txFunc.Up(ctx, conn)
}

func (m *txFuncMigrationStep) Down(ctx context.Context, conn *pgx.Conn) error {
	return m.txFunc.Down(ctx, conn)
}
