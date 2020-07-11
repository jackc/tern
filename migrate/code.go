package migrate

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
)

type CodeInstallPgError struct {
	CodeFile string
	SQL      string
	*pgconn.PgError
}

type CodePackage struct {
	schema   string
	tmpl     *template.Template
	manifest []string
}

func (cp *CodePackage) EvalAll(data map[string]interface{}) (string, error) {
	buf := &bytes.Buffer{}

	for _, s := range cp.manifest {
		fmt.Fprintf(buf, "-- %s\n\n", s)
		sql, err := cp.Eval(s, data)
		if err != nil {
			return "", err
		}
		buf.WriteString(sql)
	}

	return buf.String(), nil
}

func (cp *CodePackage) Eval(tmplName string, data map[string]interface{}) (string, error) {
	tmpl := cp.tmpl.Lookup(tmplName)
	if tmpl == nil {
		return "", fmt.Errorf("cannot find template %s", tmplName)
	}

	buf := &bytes.Buffer{}
	err := tmpl.Execute(buf, data)
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

func loadManifest(dirname string, fs http.FileSystem) ([]string, error) {
	buf, err := fsReadFile(fs, filepath.Join(dirname, "manifest.conf"))
	if err != nil {
		return nil, err
	}

	var filePaths []string

	scanner := bufio.NewScanner(bytes.NewBuffer(buf))
	for scanner.Scan() {
		s := scanner.Text()
		s = strings.TrimSpace(s)
		if len(s) > 0 && s[0] != '#' {
			filePaths = append(filePaths, s)
		}
	}

	return filePaths, nil
}

func LoadCodePackageEx(path string, fs http.FileSystem) (*CodePackage, error) {
	path = normalizeDirPath(path)

	manifest, err := loadManifest(path, fs)
	if err != nil {
		return nil, fmt.Errorf("unable to load manifest: %v", err)
	}

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

	codePackage := &CodePackage{schema: filepath.Base(path), tmpl: mainTmpl, manifest: manifest}

	return codePackage, nil
}

func LoadCodePackage(path string) (*CodePackage, error) {
	return LoadCodePackageEx(path, defaultMigratorFS{})
}

func InstallCodePackage(ctx context.Context, conn *pgx.Conn, mergeData map[string]interface{}, codePackage *CodePackage) (err error) {
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

	_, err = tx.Exec(ctx, fmt.Sprintf("drop schema if exists %s cascade", codePackage.schema))
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, fmt.Sprintf("create schema %s", codePackage.schema))
	if err != nil {
		return err
	}

	var searchPath string
	err = tx.QueryRow(ctx, "show search_path").Scan(&searchPath)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("set local search_path to %s, %s", codePackage.schema, searchPath))
	if err != nil {
		return err
	}

	for _, s := range codePackage.manifest {
		sql, err := codePackage.Eval(s, mergeData)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, sql)
		if err != nil {
			if err, ok := err.(*pgconn.PgError); ok {
				return CodeInstallPgError{CodeFile: s, SQL: sql, PgError: err}
			}
			return err
		}
	}

	return tx.Commit(ctx)
}
