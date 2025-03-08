package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	migrationPattern = regexp.MustCompile(`\A(\d+)_.+\.sql\z`)
	disableTxPattern = regexp.MustCompile(`(?m)^---- tern: disable-tx ----$`)
)

var ErrNoFwMigration = errors.New("no sql in forward migration step")

type BadVersionError string

func (e BadVersionError) Error() string {
	return string(e)
}

type IrreversibleMigrationError struct {
	m MigrationStep
}

func (e IrreversibleMigrationError) Error() string {
	return fmt.Sprintf("Irreversible migration: %d - %s", e.m.Sequence(), e.m.Name())
}

type NoMigrationsFoundError struct{}

func (e NoMigrationsFoundError) Error() string {
	return "migrations not found"
}

type MigrationPgError struct {
	MigrationName string
	Sql           string
	*pgconn.PgError
}

func (e MigrationPgError) Error() string {
	if e.MigrationName == "" {
		return e.PgError.Error()
	}
	return fmt.Sprintf("%s: %s", e.MigrationName, e.PgError.Error())
}

func (e MigrationPgError) Unwrap() error {
	return e.PgError
}

// Deprecated: use [SQLStep] or [FuncStep].
type Migration struct {
	Sequence int32
	Name     string
	UpSQL    string
	DownSQL  string
}

type MigratorOptions struct {
	// DisableTx causes the Migrator not to run migrations in a transaction.
	DisableTx bool
}

type Migrator struct {
	conn         *pgx.Conn
	versionTable string
	options      *MigratorOptions
	// Deprecated: use [Migrator.Steps].
	Migrations []*Migration
	// Steps are the migration state transitions that the [Migrator] is set up to perform.
	Steps   []MigrationStep
	OnStart func(int32, string, string, string) // OnStart is called when a migration is run with the sequence, name, direction, and SQL
	Data    map[string]interface{}              // Data available to use in migrations
}

// NewMigrator initializes a new Migrator. It is highly recommended that versionTable be schema qualified.
func NewMigrator(ctx context.Context, conn *pgx.Conn, versionTable string) (m *Migrator, err error) {
	return NewMigratorEx(ctx, conn, versionTable, &MigratorOptions{})
}

// NewMigratorEx initializes a new Migrator. It is highly recommended that versionTable be schema qualified.
func NewMigratorEx(ctx context.Context, conn *pgx.Conn, versionTable string, opts *MigratorOptions) (m *Migrator, err error) {
	m = &Migrator{conn: conn, versionTable: versionTable, options: opts}

	// This is a bit of a kludge for the gengen command. A migrator without a conn is normally not allowed. However, the
	// gengen command doesn't call any of the methods that require a conn. Potentially, we could refactor Migrator to
	// split out the migration loading and parsing from the actual migration execution.
	if conn != nil {
		err = m.ensureSchemaVersionTableExists(ctx)
	}
	m.Migrations = make([]*Migration, 0)
	m.Data = make(map[string]interface{})
	return
}

// FindMigrations finds all migration files in fsys.
func FindMigrations(fsys fs.FS) ([]string, error) {
	fileInfos, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(fileInfos))

	for _, fi := range fileInfos {
		if fi.IsDir() {
			continue
		}

		matches := migrationPattern.FindStringSubmatch(fi.Name())
		if len(matches) != 2 {
			continue
		}

		n, err := strconv.ParseInt(matches[1], 10, 32)
		if err != nil {
			// The regexp already validated that the prefix is all digits so this *should* never fail
			return nil, err
		}

		if n-1 < int64(len(paths)) && paths[n-1] != "" {
			return nil, fmt.Errorf("Duplicate migration %d", n)
		}

		// Set at specific index, so that paths are properly sorted
		paths = setAt(paths, fi.Name(), n-1)
	}

	for i, path := range paths {
		if path == "" {
			return nil, fmt.Errorf("Missing migration %d", i+1)
		}
	}

	return paths, nil
}

func (m *Migrator) LoadMigrations(fsys fs.FS) error {
	mainTmpl := template.New("main").Funcs(sprig.TxtFuncMap()).Funcs(
		template.FuncMap{
			"install_snapshot": func(name string) (string, error) {
				codePackageFSys, err := fs.Sub(fsys, "snapshots/"+name)
				if err != nil {
					return "", err
				}
				codePackage, err := LoadCodePackage(codePackageFSys)
				if err != nil {
					return "", err
				}

				return codePackage.Eval(m.Data)
			},
		},
	)

	var sharedPaths []string
	fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if (!d.IsDir()) &&
			(filepath.Dir(path) != ".") &&
			(filepath.Ext(path) == ".sql") {
			sharedPaths = append(sharedPaths, path)
		}
		return nil
	})

	for _, p := range sharedPaths {
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}

		_, err = mainTmpl.New(p).Parse(string(body))
		if err != nil {
			return err
		}
	}

	paths, err := FindMigrations(fsys)
	if err != nil {
		return err
	}

	if len(paths) == 0 {
		return NoMigrationsFoundError{}
	}

	for _, p := range paths {
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}

		pieces := strings.SplitN(string(body), "---- create above / drop below ----", 2)
		var upSQL, downSQL string
		upSQL = strings.TrimSpace(pieces[0])
		upSQL, err = m.evalMigration(mainTmpl.New(filepath.Base(p)+" up"), upSQL)
		if err != nil {
			return err
		}
		// Make sure there is SQL in the forward migration step.
		containsSQL := false
		for _, v := range strings.Split(upSQL, "\n") {
			// Only account for regular single line comment, empty line and space/comment combination
			cleanString := strings.TrimSpace(v)
			if len(cleanString) != 0 &&
				!strings.HasPrefix(cleanString, "--") {
				containsSQL = true
				break
			}
		}
		if !containsSQL {
			return ErrNoFwMigration
		}

		if len(pieces) == 2 {
			downSQL = strings.TrimSpace(pieces[1])
			downSQL, err = m.evalMigration(mainTmpl.New(filepath.Base(p)+" down"), downSQL)
			if err != nil {
				return err
			}
		}

		m.AppendMigration(filepath.Base(p), upSQL, downSQL)
	}

	return nil
}

func (m *Migrator) evalMigration(tmpl *template.Template, sql string) (string, error) {
	tmpl, err := tmpl.Parse(sql)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, m.Data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Deprecated: use [Migrator.AppendSteps].
func (m *Migrator) AppendMigration(name, upSQL, downSQL string) {
	m.Migrations = append(
		m.Migrations,
		&Migration{
			Sequence: int32(len(m.Migrations)) + 1,
			Name:     name,
			UpSQL:    upSQL,
			DownSQL:  downSQL,
		})
	return
}

// AppendSteps adds [MigrationStep] instances to the [Migrator].
func (m *Migrator) AppendSteps(steps ...MigrationStep) {
	m.Steps = append(m.Steps, steps...)
}

// Migrate runs all pending migrations.
// It calls m.OnStart when it begins a migration
func (m *Migrator) Migrate(ctx context.Context) error {
	return m.MigrateTo(ctx, m.highestSequenceNum())
}

// validate returns an error if the [Migrator] is set up in an incoherent way.
func (m *Migrator) validate() error {
	if len(m.Migrations) > 0 && len(m.Steps) > 0 {
		return fmt.Errorf("must define either Migrations or Steps, not both")
	}
	return nil
}

// Lock to ensure multiple migrations cannot occur simultaneously
const lockNum = int64(9628173550095224) // arbitrary random number

func acquireAdvisoryLock(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, "select pg_advisory_lock($1)", lockNum)
	return err
}

func releaseAdvisoryLock(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, "select pg_advisory_unlock($1)", lockNum)
	return err
}

// MigrateTo migrates to targetVersion
func (m *Migrator) MigrateTo(ctx context.Context, targetVersion int32) (err error) {
	if err := m.validate(); err != nil {
		return err
	}
	steps := m.listMigrationSteps()

	err = acquireAdvisoryLock(ctx, m.conn)
	if err != nil {
		return err
	}
	defer func() {
		unlockErr := releaseAdvisoryLock(ctx, m.conn)
		if err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	currentVersion, err := m.GetCurrentVersion(ctx)
	if err != nil {
		return err
	}

	if targetVersion < 0 || int32(len(steps)) < targetVersion {
		errMsg := fmt.Sprintf("destination version %d is outside the valid versions of 0 to %d", targetVersion, len(steps))
		return BadVersionError(errMsg)
	}

	if currentVersion < 0 || int32(len(steps)) < currentVersion {
		errMsg := fmt.Sprintf("current version %d is outside the valid versions of 0 to %d", currentVersion, len(steps))
		return BadVersionError(errMsg)
	}

	var direction int32
	if currentVersion < targetVersion {
		direction = 1
	} else {
		direction = -1
	}

	for currentVersion != targetVersion {
		var current MigrationStep
		var directionName string
		var sequence int32
		var migrationFunc func(context.Context, *pgx.Conn) error
		if direction == 1 {
			current = steps[currentVersion]
			sequence = current.Sequence()
			directionName = "up"
			migrationFunc = current.Up
		} else {
			current = steps[currentVersion-1]
			sequence = current.Sequence() - 1
			directionName = "down"
			migrationFunc = current.Down
		}

		// Fire on start callback for an SQL step.
		if sqlStep := asSQLStep(current); sqlStep != nil && m.OnStart != nil {
			m.OnStart(sequence, current.Name(), directionName, getSQL(sqlStep, directionName))
		}

		useTx := !m.options.DisableTx && !current.DisableTx()
		var tx pgx.Tx
		if useTx {
			if tx, err = m.conn.Begin(ctx); err != nil {
				return err
			}
			defer tx.Rollback(ctx)
		}

		// Execute the migration
		if err := migrationFunc(ctx, m.conn); err != nil {
			return err
		}

		// Reset all database connection settings. Important to do before updating version as search_path may have been changed.
		m.conn.Exec(ctx, "reset all")

		// Add one to the version
		_, err = m.conn.Exec(ctx, "update "+m.versionTable+" set version=$1", sequence)
		if err != nil {
			return err
		}

		if useTx {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
		}

		currentVersion = currentVersion + direction
	}

	return nil
}

func (m *Migrator) GetCurrentVersion(ctx context.Context) (v int32, err error) {
	err = m.conn.QueryRow(ctx, "select version from "+m.versionTable).Scan(&v)
	return v, err
}

func (m *Migrator) ensureSchemaVersionTableExists(ctx context.Context) (err error) {
	err = acquireAdvisoryLock(ctx, m.conn)
	if err != nil {
		return err
	}
	defer func() {
		unlockErr := releaseAdvisoryLock(ctx, m.conn)
		if err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	if ok, err := m.versionTableExists(ctx); err != nil || ok {
		return err
	}

	_, err = m.conn.Exec(ctx, fmt.Sprintf(`
    create table if not exists %s(version int4 not null);

    insert into %s(version)
    select 0
    where 0=(select count(*) from %s);
  `, m.versionTable, m.versionTable, m.versionTable))
	return err
}

func (m *Migrator) versionTableExists(ctx context.Context) (ok bool, err error) {
	var count int
	if i := strings.IndexByte(m.versionTable, '.'); i == -1 {
		err = m.conn.QueryRow(ctx, "select count(*) from pg_catalog.pg_class where relname=$1 and relkind='r' and pg_table_is_visible(oid)", m.versionTable).Scan(&count)
	} else {
		schema, table := m.versionTable[:i], m.versionTable[i+1:]
		err = m.conn.QueryRow(ctx, "select count(*) from pg_catalog.pg_tables where schemaname=$1 and tablename=$2", schema, table).Scan(&count)
	}
	return count > 0, err
}

// listMigrationSteps returns an ordered list of [MigrationStep] that the [Migrator] is set up to
// perform. In case the [Migrator] was set up to use a list of [Migration] instances, those are
// converted to a [MigrationStep] list.
func (m *Migrator) listMigrationSteps() []MigrationStep {
	if len(m.Migrations) == 0 {
		return m.Steps
	}
	steps := make([]MigrationStep, 0, len(m.Migrations))
	for _, it := range m.Migrations {
		steps = append(steps,
			SQLStep(SQL{
				Sequence: it.Sequence,
				Name:     it.Name,
				UpSQL:    it.UpSQL,
				DownSQL:  it.DownSQL,
			}))
	}
	return steps
}

// highestSequenceNum returns the highest sequence number of any [MigrationStep] handled by the
// [Migrator].
func (m *Migrator) highestSequenceNum() int32 {
	if len(m.Migrations) > 0 {
		return m.Migrations[len(m.Migrations)-1].Sequence
	}
	return m.Steps[len(m.Steps)-1].Sequence()
}

func setAt(strs []string, value string, pos int64) []string {
	// If pos > length - 1, append empty strings to make it the right size
	if pos > int64(len(strs))-1 {
		strs = append(strs, make([]string, 1+pos-int64(len(strs)))...)
	}
	strs[pos] = value
	return strs
}

// asSQLStep casts a [MigrationStep] to an [SQLMigrationStep]. If s is not an [SQLMigrationStep] nil
// is returned.
func asSQLStep(s MigrationStep) SQLMigrationStep {
	sqlMigration, ok := s.(SQLMigrationStep)
	if ok {
		return sqlMigration
	}
	return nil
}

// getSQL returns the SQL statement of an [SQLMigrationStep] for a given direction ("up" or "down").
func getSQL(s SQLMigrationStep, direction string) string {
	if direction == "up" {
		return s.UpSQL()
	}
	return s.DownSQL()
}
