package migrate

import (
	"io/ioutil"
	"net/http"
	"os"
)

func fsReadDir(fs http.FileSystem, dirname string) ([]os.FileInfo, error) {
	file, err := fs.Open(dirname)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return file.Readdir(0)
}

func fsReadFile(fs http.FileSystem, filename string) ([]byte, error) {
	file, err := fs.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return ioutil.ReadAll(file)
}

func fsFiles(fs http.FileSystem, path string, files []string) ([]string, error) {
	fileInfos, err := fsReadDir(fs, path)
	if err != nil {
		return nil, err
	}

	for _, fi := range fileInfos {
		fiPath := path + "/" + fi.Name()
		if fi.IsDir() {
			files, err = fsFiles(fs, fiPath, files)
			if err != nil {
				return nil, err
			}
		} else {
			files = append(files, fiPath)
		}
	}

	return files, nil
}
