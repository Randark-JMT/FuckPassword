package api

import (
	"io/fs"
	"net/http"
	"strings"

	"fuckpassword/web"
)

func NewRouter(a *API) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/upload", a.HandleUpload)
	mux.HandleFunc("GET /api/upload/status", a.HandleUploadStatus)
	mux.HandleFunc("POST /api/jobs", a.HandleSubmit)
	mux.HandleFunc("GET /api/jobs", a.HandleBoard)
	mux.HandleFunc("GET /api/jobs/{id}", a.HandleJob)
	mux.HandleFunc("GET /api/jobs/{id}/results", a.HandleResults)
	mux.HandleFunc("GET /api/jobs/{id}/download", a.HandleDownload)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", a.HandleCancel)

	distFS := web.DistFS()
	fileServer := http.FileServer(http.FS(distFS))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
			return
		}
		if _, err := fs.Stat(distFS, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	return mux
}
