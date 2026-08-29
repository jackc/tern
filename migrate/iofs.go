// +build go1.16

package migrate

import (
	"io/fs"
	"os"
)

// NewFS returns a MigratorFS that uses as fs.FS filesystem.
func NewFS(fsys fs.FS) MigratorFS {
	return iofsMigratorFS{fsys: fsys}
}

type iofsMigratorFS struct{ fsys fs.FS }

// ReadDir implements the MigratorFS interface.
func (m iofsMigratorFS) ReadDir(dirname string) ([]fs.FileInfo, error) {
	d, err := fs.ReadDir(m.fsys, dirname)
	if err != nil {
		return nil, err
	}
	var fis []os.FileInfo
	for _, v := range d {
		fi, err := v.Info()
		if err != nil {
			return nil, err
		}
		fis = append(fis, fi)
	}
	return fis, nil
}

// ReadFile implements the MigratorFS interface.
func (m iofsMigratorFS) ReadFile(filename string) ([]byte, error) {
	return fs.ReadFile(m.fsys, filename)
}

// Glob implements the MigratorFS interface.
func (m iofsMigratorFS) Glob(pattern string) (matches []string, err error) {
	return fs.Glob(m.fsys, pattern)
}
