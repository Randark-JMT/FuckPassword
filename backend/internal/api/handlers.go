package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"fuckpassword/internal/db"
	"fuckpassword/internal/ingest"
)

const (
	maxPatternRunes = 500
	maxResultLimit  = 1000
)

type API struct {
	DB       *db.DB
	Ingest   *ingest.Service
	MaxQueue int
}

func (a *API) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if a.Ingest.IsBusy() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "an Upload is already in progress"})
		return
	}
	if err := a.Ingest.StartUpload(r.Body, r.ContentLength); err != nil {
		if errors.Is(err, ingest.ErrBusy) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "an Upload is already in progress"})
			return
		}
		log.Printf("upload phase A failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"phase":       ingest.PhaseProcessing,
		"bytes_total": r.ContentLength,
	})
}

func (a *API) HandleUploadStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.Ingest.Snapshot())
}

type submitRequest struct {
	Pattern string `json:"pattern"`
	IsRegex bool   `json:"is_regex"`
}

func (a *API) HandleSubmit(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	req.Pattern = strings.TrimSpace(req.Pattern)
	if req.Pattern == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "pattern is empty"})
		return
	}
	if len([]rune(req.Pattern)) > maxPatternRunes {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "pattern too long"})
		return
	}
	id, err := a.DB.EnqueueJob(r.Context(), req.Pattern, req.IsRegex, a.MaxQueue)
	if err != nil {
		if errors.Is(err, db.ErrQueueFull) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "queue full"})
			return
		}
		log.Printf("enqueue failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task_id": id, "status": "queued"})
}

func (a *API) HandleBoard(w http.ResponseWriter, r *http.Request) {
	running, queued, err := a.DB.Board(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": running, "queued": queued})
}

func (a *API) HandleJob(w http.ResponseWriter, r *http.Request) {
	job, err := a.DB.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (a *API) HandleResults(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := a.DB.GetJob(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	if job.Status != "completed" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "job not completed", "status": job.Status})
		return
	}
	offset := atoiDefault(r.URL.Query().Get("offset"), 0)
	limit := atoiDefault(r.URL.Query().Get("limit"), maxResultLimit)
	if limit > maxResultLimit {
		limit = maxResultLimit
	}
	rows, err := a.DB.FetchResults(r.Context(), id, offset, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	total, _ := a.DB.CountResults(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"results":   rows,
		"count":     len(rows),
		"total":     total,
		"truncated": total > offset+limit,
	})
}

func (a *API) HandleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := a.DB.GetJob(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	if job.Status != "completed" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "job not completed", "status": job.Status})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="results.txt"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := a.DB.StreamResultsCopy(r.Context(), id, w); err != nil {
		log.Printf("download stream: %v", err)
	}
}

func (a *API) HandleCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wasRunning, pid, err := a.DB.CancelJob(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if wasRunning && pid != 0 {
		if _, err := a.DB.Pool.Exec(r.Context(), "SELECT pg_cancel_backend($1)", pid); err != nil {
			log.Printf("pg_cancel_backend %d: %v", pid, err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": true})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}
