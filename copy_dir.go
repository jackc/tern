package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

// it would be really nice if there were a standard Go fileutils package...
func copyCodePackageDir(src, dst string) error {
	err := os.MkdirAll(dst, os.ModeDir|os.ModePerm)
	if err != nil {
		return err
	}

	entries, err := ioutil.ReadDir(src)
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
			data, err := ioutil.ReadFile(srcPath)
			if err != nil {
				return err
			}

			err = ioutil.WriteFile(dstPath, data, e.Mode().Perm())
			if err != nil {
				return err
			}
		}
	}

	return nil
}
