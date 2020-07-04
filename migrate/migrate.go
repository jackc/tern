package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v4"
)

var migrationPattern = regexp.MustCompile(`\A(\d+)_.+\.sql\z`)

var ErrNoFwMigration = errors.New("no sql in forward migration step")

type BadVersionError string

func (e BadVersionError) Error() string {
	return string(e)
}

type IrreversibleMigrationError struct {
	m *Migration
}

func (e IrreversibleMigrationError) Error() string {
	return fmt.Sprintf("Irreversible migration: %d - %s", e.m.Sequence, e.m.Name)
}

type NoMigrationsFoundError struct {
	Path string
}

func (e NoMigrationsFoundError) Error() string {
	return fmt.Sprintf("No migrations found at %s", e.Path)
}

type MigrationPgError struct {
	Sql string
	*pgconn.PgError
}

type Migration struct {
	Sequence int32
	Name     string
	UpSQL    string
}

type MigratorOptions struct {
	// DisableTx causes the Migrator not to run migrations in a transaction.
	DisableTx bool
	// MigratorFS is the interface used for collecting the migrations.
	MigratorFS MigratorFS
}

type Migrator struct {
	conn         *pgx.Conn
	versionTable string
	options      *MigratorOptions
	Migrations   []*Migration
	OnStart      func(sequence int32, name string, sql string) // OnStart is called when a migration is run.
	Data         map[string]interface{}                        // Data available to use in migrations
}

// NewMigrator initializes a new Migrator. It is highly recommended that versionTable be schema qualified.
func NewMigrator(ctx context.Context, conn *pgx.Conn, versionTable string) (m *Migrator, err error) {
	return NewMigratorEx(ctx, conn, versionTable, &MigratorOptions{MigratorFS: defaultMigratorFS{}})
}

// NewMigratorEx initializes a new Migrator. It is highly recommended that versionTable be schema qualified.
func NewMigratorEx(ctx context.Context, conn *pgx.Conn, versionTable string, opts *MigratorOptions) (m *Migrator, err error) {
	m = &Migrator{conn: conn, versionTable: versionTable, options: opts}
	err = m.ensureSchemaVersionTableExists(ctx)
	m.Migrations = make([]*Migration, 0)
	m.Data = make(map[string]interface{})
	return
}

type MigratorFS interface {
	ReadDir(dirname string) ([]os.FileInfo, error)
	ReadFile(filename string) ([]byte, error)
	Glob(pattern string) (matches []string, err error)
}

type defaultMigratorFS struct{}

func (defaultMigratorFS) ReadDir(dirname string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(dirname)
}

func (defaultMigratorFS) ReadFile(filename string) ([]byte, error) {
	return ioutil.ReadFile(filename)
}

func (defaultMigratorFS) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

func FindMigrationsEx(path string, fs MigratorFS) ([]string, error) {
	path = strings.TrimRight(path, string(filepath.Separator))

	fileInfos, err := fs.ReadDir(path)
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

		if n < int64(len(paths)+1) {
			return nil, fmt.Errorf("Duplicate migration %d", n)
		}

		if int64(len(paths)+1) < n {
			return nil, fmt.Errorf("Missing migration %d", len(paths)+1)
		}

		paths = append(paths, filepath.Join(path, fi.Name()))
	}

	return paths, nil
}

func FindMigrations(path string) ([]string, error) {
	return FindMigrationsEx(path, defaultMigratorFS{})
}

func (m *Migrator) LoadMigrations(path string) error {
	path = strings.TrimRight(path, string(filepath.Separator))

	mainTmpl := template.New("main").Funcs(sprig.TxtFuncMap()).Funcs(
		template.FuncMap{
			"install_snapshot": func(name string) (string, error) {
				codePackage, err := LoadCodePackageEx(filepath.Join(path, "snapshots", name), m.options.MigratorFS)
				if err != nil {
					return "", err
				}

				return codePackage.Eval(m.Data)
			},
		},
	)

	sharedPaths, err := m.options.MigratorFS.Glob(filepath.Join(path, "*", "*.sql"))
	if err != nil {
		return err
	}

	for _, p := range sharedPaths {
		body, err := m.options.MigratorFS.ReadFile(p)
		if err != nil {
			return err
		}

		name := strings.Replace(p, path+string(filepath.Separator), "", 1)
		_, err = mainTmpl.New(name).Parse(string(body))
		if err != nil {
			return err
		}
	}

	paths, err := FindMigrationsEx(path, m.options.MigratorFS)
	if err != nil {
		return err
	}

	if len(paths) == 0 {
		return NoMigrationsFoundError{Path: path}
	}

	for _, p := range paths {
		body, err := m.options.MigratorFS.ReadFile(p)
		if err != nil {
			return err
		}

		upSQL := strings.TrimSpace(string(body))
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

		m.AppendMigration(filepath.Base(p), upSQL)
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

func (m *Migrator) AppendMigration(name, upSQL string) {
	m.Migrations = append(
		m.Migrations,
		&Migration{
			Sequence: int32(len(m.Migrations)) + 1,
			Name:     name,
			UpSQL:    upSQL,
		})
	return
}

// Migrate runs pending migrations
// It calls m.OnStart when it begins a migration
func (m *Migrator) Migrate(ctx context.Context) error {
	return m.MigrateTo(ctx, int32(len(m.Migrations)))
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

	if currentVersion < 0 {
		errMsg := fmt.Sprintf("current version %d is less than 0", currentVersion)
		return BadVersionError(errMsg)
	}

	if int32(len(m.Migrations)) < currentVersion {
		errMsg := fmt.Sprintf("current version %d is greater than last version of %d", currentVersion, len(m.Migrations))
		return BadVersionError(errMsg)
	}

	if targetVersion < currentVersion || int32(len(m.Migrations)) < targetVersion {
		errMsg := fmt.Sprintf("destination version %d is outside the valid versions of %d to %d", targetVersion, currentVersion, len(m.Migrations))
		return BadVersionError(errMsg)
	}

	for ; currentVersion != targetVersion; currentVersion++ {
		var current *Migration
		var sql string
		var sequence int32
		current = m.Migrations[currentVersion]
		sequence = current.Sequence
		sql = current.UpSQL

		var tx pgx.Tx
		if !m.options.DisableTx {
			tx, err = m.conn.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
		}

		// Fire on start callback
		if m.OnStart != nil {
			m.OnStart(current.Sequence, current.Name, sql)
		}

		// Execute the migration
		_, err = m.conn.Exec(ctx, sql)
		if err != nil {
			if err, ok := err.(*pgconn.PgError); ok {
				return MigrationPgError{Sql: sql, PgError: err}
			}
			return err
		}

		// Reset all database connection settings. Important to do before updating version as search_path may have been changed.
		m.conn.Exec(ctx, "reset all")

		// Add one to the version
		_, err = m.conn.Exec(ctx, "update "+m.versionTable+" set version=$1", sequence)
		if err != nil {
			return err
		}

		if !m.options.DisableTx {
			err = tx.Commit(ctx)
			if err != nil {
				return err
			}
		}
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

	_, err = m.GetCurrentVersion(ctx)
	if err == nil {
		return nil
	}
	if pgErr, ok := err.(*pgconn.PgError); !ok || pgErr.Code != pgerrcode.UndefinedTable {
		return err
	}

	_, err = m.conn.Exec(ctx, fmt.Sprintf(`
    create table if not exists %[1]s(
			version int4 not null check (version >= 0)
		);

		create unique index on %[1]s (((1)));

		insert into %[1]s(version) values (0)
		on conflict do nothing;
  `, m.versionTable))
	return err
}
