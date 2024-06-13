package main

import (
	"os"
	"path/filepath"
)

// it would be really nice if there were a standard Go fileutils package...
func copyCodePackageDir(src, dst string) error {
	err := os.MkdirAll(dst, os.ModeDir|os.ModePerm)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			err := copyCodePackageDir(srcPath, dstPath)
			if err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}

			fileInfo, err := os.Stat(srcPath)
			if err != nil {
				return err
			}

			err = os.WriteFile(dstPath, data, fileInfo.Mode().Perm())
			if err != nil {
				return err
			}
		}
	}

	return nil
}
