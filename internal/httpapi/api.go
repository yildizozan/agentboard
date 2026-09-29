// Package httpapi serves the web board and its JSON API on a loopback address.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yildizozan/agentboard/internal/store"
	"github.com/yildizozan/agentboard/internal/task"
)

// maxBody bounds request bodies; task payloads are tiny.
const maxBody = 1 << 20

// placeholder is served when the UI was not built into the binary.
const placeholder = `<!doctype html><title>agentboard</title>
<p>The board UI is not built into this binary. Build it with:</p>
<pre>npm --prefix web ci &amp;&amp; npm --prefix web run build</pre>
<p>then rebuild agentboard. The API under /api works without it.</p>`

type repoJSON struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type tasksJSON struct {
	Statuses []task.Status `json:"statuses"`
	Tasks    []task.Task   `json:"tasks"`
}

type createJSON struct {
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Status      task.Status `json:"status"`
}

type errorJSON struct {
	Error   string      `json:"error"`
	Current task.Status `json:"current,omitempty"`
}

// New returns the board handler. ui holds the built frontend; without an index.html a
// placeholder page explains how to build it.
func New(s *store.Store, ui fs.FS) http.Handler {
	a := api{store: s}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/repos", a.repos)
	mux.HandleFunc("GET /api/tasks", a.listTasks)
	mux.HandleFunc("POST /api/tasks", a.createTask)
	mux.HandleFunc("PATCH /api/tasks/{id}", a.patchTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", a.deleteTask)
	mux.Handle("GET /", uiHandler(ui))
	return guard(mux)
}

func uiHandler(ui fs.FS) http.Handler {
	if ui != nil {
		if _, err := fs.Stat(ui, "index.html"); err == nil {
			return http.FileServerFS(ui)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, placeholder)
	})
}

// guard protects the loopback server from other sites open in the browser.
// A loopback Host blocks DNS rebinding; requiring JSON bodies forces a CORS preflight,
// which fails because no CORS headers are ever sent, so cross-site writes are blocked.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			writeJSON(w, http.StatusForbidden, errorJSON{Error: "forbidden host"})
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				writeJSON(w, http.StatusUnsupportedMediaType, errorJSON{Error: "Content-Type must be application/json"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.Trim(hostport, "[]")
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type api struct {
	store *store.Store
}

func (a api) repos(w http.ResponseWriter, r *http.Request) {
	repos, err := a.store.Repos(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]repoJSON, len(repos))
	for i, rp := range repos {
		out[i] = repoJSON{Path: rp.Path, Name: filepath.Base(rp.Path), Count: rp.Count}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a api) listTasks(w http.ResponseWriter, r *http.Request) {
	repo, err := repoParam(r)
	if err != nil {
		writeError(w, err)
		return
	}
	tasks, err := a.store.List(r.Context(), repo, nil)
	if err != nil {
		writeError(w, err)
		return
	}
	if tasks == nil {
		tasks = []task.Task{}
	}
	writeJSON(w, http.StatusOK, tasksJSON{Statuses: task.Statuses(), Tasks: tasks})
}

func (a api) createTask(w http.ResponseWriter, r *http.Request) {
	repo, err := repoParam(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var in createJSON
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	if in.Status == "" {
		in.Status = task.Backlog
	}
	tk, err := a.store.Add(r.Context(), repo, in.Title, in.Description, in.Status)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tk)
}

func (a api) patchTask(w http.ResponseWriter, r *http.Request) {
	repo, id, err := repoAndID(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var p task.Patch
	if err := decodeJSON(r, &p); err != nil {
		writeError(w, err)
		return
	}
	tk, err := a.store.Update(r.Context(), repo, id, p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tk)
}

func (a api) deleteTask(w http.ResponseWriter, r *http.Request) {
	repo, id, err := repoAndID(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := a.store.Delete(r.Context(), repo, id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func repoParam(r *http.Request) (string, error) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		return "", task.Invalidf("repo query parameter is required")
	}
	return repo, nil
}

func repoAndID(r *http.Request) (string, int64, error) {
	repo, err := repoParam(r)
	if err != nil {
		return "", 0, err
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return "", 0, task.Invalidf("invalid task id %q", r.PathValue("id"))
	}
	return repo, id, nil
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return task.Invalidf("invalid JSON body: %v", err)
	}
	return nil
}

// writeError maps domain errors to HTTP status codes in one place.
func writeError(w http.ResponseWriter, err error) {
	var conflict *task.ConflictError
	switch {
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, errorJSON{Error: err.Error(), Current: conflict.Current})
	case errors.Is(err, task.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorJSON{Error: err.Error()})
	case errors.Is(err, task.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, errorJSON{Error: err.Error()})
	default:
		log.Printf("agentboard board: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorJSON{Error: "internal error"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
