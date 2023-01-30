package utils

import (
	"embed"
	"io/fs"
	"net/http"
)

func HTTPFileserver(embeddedFS embed.FS) (http.Handler, error) {
	fsys, err := fs.Sub(embeddedFS, ".")
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(fsys)), nil
}
