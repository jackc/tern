package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
)

type CodePackage struct {
	tmpl *template.Template
}

func (cp *CodePackage) Eval(data map[string]interface{}) (string, error) {
	buf := &bytes.Buffer{}
	err := cp.tmpl.Lookup("install.sql").Execute(buf, data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func findCodeFiles(dirname string, fs http.FileSystem) ([]string, error) {
	dirname = strings.TrimRight(dirname, string(filepath.Separator))

	entries, err := fsReadDir(fs, dirname)
	if err != nil {
		return nil, err
	}

	var results []string

	for _, e := range entries {
		ePath := filepath.Join(dirname, e.Name())
		if e.IsDir() {
			paths, err := findCodeFiles(ePath, fs)
			if err != nil {
				return nil, err
			}
			results = append(results, paths...)
		} else {
			match, err := filepath.Match("*.sql", e.Name())
			if err != nil {
				return nil, fmt.Errorf("impossible filepath.Match error %w", err)
			}
			if match {
				results = append(results, ePath)
			}
		}
	}

	return results, nil
}

func LoadCodePackageEx(path string, fs http.FileSystem) (*CodePackage, error) {
	path = strings.TrimRight(path, string(filepath.Separator))

	mainTmpl := template.New("main").Funcs(sprig.TxtFuncMap())
	sqlPaths, err := findCodeFiles(path, fs)
	if err != nil {
		return nil, err
	}

	for _, p := range sqlPaths {
		body, err := fsReadFile(fs, p)
		if err != nil {
			return nil, err
		}

		name := strings.Replace(p, path+string(filepath.Separator), "", 1)
		_, err = mainTmpl.New(name).Parse(string(body))
		if err != nil {
			return nil, err
		}
	}

	installTmpl := mainTmpl.Lookup("install.sql")
	if installTmpl == nil {
		return nil, errors.New("install.sql not found")
	}

	codePackage := &CodePackage{tmpl: mainTmpl}

	return codePackage, nil
}

func LoadCodePackage(path string) (*CodePackage, error) {
	return LoadCodePackageEx(path, defaultMigratorFS{})
}

func InstallCodePackage(ctx context.Context, conn *pgx.Conn, mergeData map[string]interface{}, codePackage *CodePackage) (err error) {
	sql, err := codePackage.Eval(mergeData)
	if err != nil {
		return err
	}

	return LockExecTx(ctx, conn, sql)
}

func LockExecTx(ctx context.Context, conn *pgx.Conn, sql string) (err error) {
	err = acquireAdvisoryLock(ctx, conn)
	if err != nil {
		return err
	}
	defer func() {
		unlockErr := releaseAdvisoryLock(ctx, conn)
		if err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, sql)
	if err != nil {
		if err, ok := err.(*pgconn.PgError); ok {
			return MigrationPgError{Sql: sql, PgError: err}
		}
		return err
	}

	return tx.Commit(ctx)

}
