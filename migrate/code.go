package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func findCodeFiles(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}

	var results []string

	for _, e := range entries {
		if e.IsDir() {
			subfs, err := fs.Sub(fsys, e.Name())
			if err != nil {
				return nil, err
			}
			paths, err := findCodeFiles(subfs)
			if err != nil {
				return nil, err
			}

			for _, p := range paths {
				results = append(results, filepath.Join(e.Name(), p))
			}
		} else {
			match, err := filepath.Match("*.sql", e.Name())
			if err != nil {
				return nil, fmt.Errorf("impossible filepath.Match error %w", err)
			}
			if match {
				results = append(results, e.Name())
			}
		}
	}

	return results, nil
}

func LoadCodePackage(fsys fs.FS) (*CodePackage, error) {
	mainTmpl := template.New("main").Funcs(sprig.TxtFuncMap())
	sqlPaths, err := findCodeFiles(fsys)
	if err != nil {
		return nil, err
	}

	for _, p := range sqlPaths {
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}

		_, err = mainTmpl.New(p).Parse(string(body))
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
