package http

import (
	"net/http"
	"os"

	assetfs "github.com/elazarl/go-bindata-assetfs"
)

type UIAssetWrapper struct {
	FileSystem *assetfs.AssetFS
}

func (fs *UIAssetWrapper) Open(name string) (http.File, error) {
	file, err := fs.FileSystem.Open(name)

	if err == nil {
		return file, nil
	}

	// serve index.html instead of 404ing
	if err == os.ErrNotExist {
		return fs.FileSystem.Open("index.html")
	}
	return nil, err
}
