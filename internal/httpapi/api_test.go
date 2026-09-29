package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yildizozan/agentboard/internal/store"
	"github.com/yildizozan/agentboard/internal/task"
)

const repoA, repoB = "/work/a", "/work/b"

type env struct {
	h     http.Handler
	store *store.Store
}

func newEnv(t *testing.T, ui fstest.MapFS) env {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "agentboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return env{h: New(s, ui), store: s}
}

// do sends a request as the board page would: loopback Host and JSON bodies.
func (e env) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	req.Host = "127.0.0.1:7420"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e env) add(t *testing.T, repo, body string, status task.Status) task.Task {
	t.Helper()
	tk, err := e.store.Add(context.Background(), repo, body, status)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func tasksURL(repo string) string { return "/api/tasks?repo=" + url.QueryEscape(repo) }

func taskURL(id int64, repo string) string {
	return "/api/tasks/" + jsonNumber(id) + "?repo=" + url.QueryEscape(repo)
}

func jsonNumber(id int64) string { b, _ := json.Marshal(id); return string(b) }

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, want, rec.Body.String())
	}
}

func TestRepos(t *testing.T) {
	e := newEnv(t, nil)
	e.add(t, repoA, "x", task.Todo)
	e.add(t, repoA, "y", task.Done)
	rec := e.do(t, "GET", "/api/repos", "")
	expectStatus(t, rec, 200)
	got := decode[[]repoJSON](t, rec)
	if len(got) != 1 || got[0].Path != repoA || got[0].Name != "a" || got[0].Count != 2 {
		t.Errorf("repos = %+v", got)
	}
}

func TestListTasksIncludesStatusesInOrder(t *testing.T) {
	e := newEnv(t, nil)
	e.add(t, repoA, "done too", task.Done)
	e.add(t, repoB, "other repo", task.Todo)
	rec := e.do(t, "GET", tasksURL(repoA), "")
	expectStatus(t, rec, 200)
	got := decode[tasksJSON](t, rec)
	if strings.Join(statusNames(got.Statuses), ",") != "backlog,todo,doing,done" {
		t.Errorf("statuses = %v", got.Statuses)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].Title() != "done too" {
		t.Errorf("tasks = %+v", got.Tasks)
	}
	expectStatus(t, e.do(t, "GET", "/api/tasks", ""), 400)
}

func statusNames(ss []task.Status) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	return out
}

func TestCreateTask(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.do(t, "POST", tasksURL(repoA), `{"body":"New card\n\n## Context\nd"}`)
	expectStatus(t, rec, 201)
	got := decode[map[string]any](t, rec)
	if got["title"] != "New card" || got["body"] != "# New card\n\n## Context\nd" || got["status"] != "backlog" || got["repo"] != repoA {
		t.Errorf("created = %+v", got)
	}
	expectStatus(t, e.do(t, "POST", tasksURL(repoA), `{"body":"  "}`), 400)
	expectStatus(t, e.do(t, "POST", tasksURL(repoA), `{"body":"## Context"}`), 400)
	expectStatus(t, e.do(t, "POST", tasksURL(repoA), `{"body":"x","status":"later"}`), 400)
	expectStatus(t, e.do(t, "POST", tasksURL(repoA), `{"title":"x"}`), 400)
	expectStatus(t, e.do(t, "POST", tasksURL(repoA), `not json`), 400)
}

func TestPatchTask(t *testing.T) {
	e := newEnv(t, nil)
	tk := e.add(t, repoA, "card", task.Doing)

	rec := e.do(t, "PATCH", taskURL(tk.ID, repoA), `{"status":"done","from":"todo"}`)
	expectStatus(t, rec, 409)
	if got := decode[errorJSON](t, rec); got.Current != task.Doing {
		t.Errorf("conflict body = %+v, want current doing", got)
	}

	rec = e.do(t, "PATCH", taskURL(tk.ID, repoA), `{"body":"# renamed\n\nmore"}`)
	expectStatus(t, rec, 200)
	if got := decode[task.Task](t, rec); got.Body != "# renamed\n\nmore" || got.Status != task.Doing {
		t.Errorf("patched = %+v", got)
	}

	expectStatus(t, e.do(t, "PATCH", taskURL(tk.ID, repoB), `{"status":"done"}`), 404)
	expectStatus(t, e.do(t, "PATCH", taskURL(tk.ID, repoA), `{}`), 400)
	expectStatus(t, e.do(t, "PATCH", "/api/tasks/abc?repo=x", `{"status":"done"}`), 400)
}

func TestDeleteTask(t *testing.T) {
	e := newEnv(t, nil)
	tk := e.add(t, repoA, "card", task.Todo)
	expectStatus(t, e.do(t, "DELETE", taskURL(tk.ID, repoB), ""), 404)
	expectStatus(t, e.do(t, "DELETE", taskURL(tk.ID, repoA), ""), 204)
	expectStatus(t, e.do(t, "DELETE", taskURL(tk.ID, repoA), ""), 404)
}

func TestRejectsForeignHost(t *testing.T) {
	e := newEnv(t, nil)
	for _, host := range []string{"evil.example", "evil.example:7420", "127.0.0.1.evil.example"} {
		req := httptest.NewRequest("POST", tasksURL(repoA), strings.NewReader(`{"body":"x"}`))
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		expectStatus(t, rec, 403)
	}
	for _, host := range []string{"localhost:7420", "[::1]:7420", "127.0.0.1"} {
		req := httptest.NewRequest("GET", "/api/repos", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		expectStatus(t, rec, 200)
	}
	if repos, _ := e.store.Repos(context.Background()); len(repos) != 0 {
		t.Errorf("foreign request changed the DB: %v", repos)
	}
}

func TestWritesRequireJSON(t *testing.T) {
	e := newEnv(t, nil)
	tk := e.add(t, repoA, "card", task.Todo)
	for _, tc := range []struct{ method, target string }{
		{"POST", tasksURL(repoA)},
		{"PATCH", taskURL(tk.ID, repoA)},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(`{"body":"x"}`))
		req.Host = "127.0.0.1:7420"
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		expectStatus(t, rec, 415)
	}
	if got, _ := e.store.List(context.Background(), repoA, nil); len(got) != 1 || got[0].Title() != "card" {
		t.Errorf("non-JSON write changed the DB: %+v", got)
	}
}

func TestServesUIAndPlaceholder(t *testing.T) {
	built := newEnv(t, fstest.MapFS{"index.html": {Data: []byte("<h1>board</h1>")}})
	rec := built.do(t, "GET", "/", "")
	expectStatus(t, rec, 200)
	if !strings.Contains(rec.Body.String(), "<h1>board</h1>") {
		t.Errorf("index body = %q", rec.Body.String())
	}

	empty := newEnv(t, fstest.MapFS{".gitkeep": {}})
	rec = empty.do(t, "GET", "/", "")
	expectStatus(t, rec, 200)
	if !strings.Contains(rec.Body.String(), "npm --prefix web run build") {
		t.Errorf("placeholder body = %q", rec.Body.String())
	}
	expectStatus(t, empty.do(t, "GET", "/api/repos", ""), 200)
}
