package migrate

import (
	"bytes"
	"fmt"
	"github.com/jackc/pgx"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

var migrationPattern = regexp.MustCompile(`\A(\d+)_.+\.sql\z`)

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

type MigrationSyntaxError struct {
	Sql string
	pgx.PgError
}

type Migration struct {
	Sequence int32
	Name     string
	UpSQL    string
	DownSQL  string
}

type Migrator struct {
	conn         *pgx.Conn
	versionTable string
	Migrations   []*Migration
	OnStart      func(int32, string, string, string) // OnStart is called when a migration is run with the sequence, name, direction, and SQL
	Data         map[string]interface{}              // Data available to use in migrations
}

func NewMigrator(conn *pgx.Conn, versionTable string) (m *Migrator, err error) {
	m = &Migrator{conn: conn, versionTable: versionTable}
	err = m.ensureSchemaVersionTableExists()
	m.Migrations = make([]*Migration, 0)
	m.Data = make(map[string]interface{})
	return
}

func FindMigrations(path string) ([]string, error) {
	path = strings.TrimRight(path, string(filepath.Separator))

	fileInfos, err := ioutil.ReadDir(path)
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

func (m *Migrator) LoadMigrations(path string) error {
	path = strings.TrimRight(path, string(filepath.Separator))

	mainTmpl := template.New("main")
	sharedPaths, err := filepath.Glob(filepath.Join(path, "*", "*.sql"))
	if err != nil {
		return err
	}

	for _, p := range sharedPaths {
		body, err := ioutil.ReadFile(p)
		if err != nil {
			return err
		}

		name := strings.Replace(p, path+string(filepath.Separator), "", 1)
		_, err = mainTmpl.New(name).Parse(string(body))
		if err != nil {
			return err
		}
	}

	paths, err := FindMigrations(path)
	if err != nil {
		return err
	}

	if len(paths) == 0 {
		return NoMigrationsFoundError{Path: path}
	}

	for _, p := range paths {
		body, err := ioutil.ReadFile(p)
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

// Migrate runs pending migrations
// It calls m.OnStart when it begins a migration
func (m *Migrator) Migrate() error {
	return m.MigrateTo(int32(len(m.Migrations)))
}

// MigrateTo migrates to targetVersion
func (m *Migrator) MigrateTo(targetVersion int32) (err error) {
	// Lock to ensure multiple migrations cannot occur simultaneously
	lockNum := int64(9628173550095224) // arbitrary random number
	if _, lockErr := m.conn.Exec("select pg_advisory_lock($1)", lockNum); lockErr != nil {
		return lockErr
	}
	defer func() {
		_, unlockErr := m.conn.Exec("select pg_advisory_unlock($1)", lockNum)
		if err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	currentVersion, err := m.GetCurrentVersion()
	if err != nil {
		return err
	}

	if targetVersion < 0 || int32(len(m.Migrations)) < targetVersion {
		errMsg := fmt.Sprintf("destination version %d is outside the valid versions of 0 to %d", targetVersion, len(m.Migrations))
		return BadVersionError(errMsg)
	}

	if currentVersion < 0 || int32(len(m.Migrations)) < currentVersion {
		errMsg := fmt.Sprintf("current version %d is outside the valid versions of 0 to %d", currentVersion, len(m.Migrations))
		return BadVersionError(errMsg)
	}

	var direction int32
	if currentVersion < targetVersion {
		direction = 1
	} else {
		direction = -1
	}

	for currentVersion != targetVersion {
		var current *Migration
		var sql, directionName string
		var sequence int32
		if direction == 1 {
			current = m.Migrations[currentVersion]
			sequence = current.Sequence
			sql = current.UpSQL
			directionName = "up"
		} else {
			current = m.Migrations[currentVersion-1]
			sequence = current.Sequence - 1
			sql = current.DownSQL
			directionName = "down"
			if current.DownSQL == "" {
				return IrreversibleMigrationError{m: current}
			}
		}

		tx, err := m.conn.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Fire on start callback
		if m.OnStart != nil {
			m.OnStart(current.Sequence, current.Name, directionName, sql)
		}

		// Execute the migration
		_, err = m.conn.Exec(sql)
		if err != nil {
			if err, ok := err.(pgx.PgError); ok && err.Position != 0 {
				return MigrationSyntaxError{Sql: sql, PgError: err}
			}
			return err
		}

		// Add one to the version
		_, err = m.conn.Exec("update "+m.versionTable+" set version=$1", sequence)
		if err != nil {
			return err
		}

		err = tx.Commit()
		if err != nil {
			return err
		}

		currentVersion = currentVersion + direction
	}

	return nil
}

func (m *Migrator) GetCurrentVersion() (v int32, err error) {
	err = m.conn.QueryRow("select version from " + m.versionTable).Scan(&v)
	return v, err
}

func (m *Migrator) ensureSchemaVersionTableExists() (err error) {
	_, err = m.conn.Exec(fmt.Sprintf(`
    create table if not exists %s(version int4 not null);

    insert into %s(version)
    select 0
    where 0=(select count(*) from %s);
  `, m.versionTable, m.versionTable, m.versionTable))
	return err
}
