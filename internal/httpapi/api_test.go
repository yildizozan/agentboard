package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
	tk, err := e.store.Add(context.Background(), repo, task.Draft{Body: body, Status: status, EpicID: ptrEpic(e.epic(t, repo))})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func ptrEpic(id int64) *int64 { return &id }

func (e env) epic(t *testing.T, repo string) int64 {
	t.Helper()
	cards, err := e.store.List(context.Background(), repo, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		if card.Kind == task.EpicKind && card.Body == "# Test epic" {
			return card.ID
		}
	}
	card, err := e.store.Add(context.Background(), repo, task.Draft{Body: "# Test epic", Kind: task.EpicKind, Status: task.Done, Priority: task.Low})
	if err != nil {
		t.Fatal(err)
	}
	return card.ID
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
	if len(got) != 1 || got[0].Path != repoA || got[0].Name != "a" || got[0].Count != 3 {
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
	if len(got.Tasks) != 2 || got.Tasks[0].Title() != "done too" {
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
	epicID := e.epic(t, repoA)
	rec := e.do(t, "POST", tasksURL(repoA), `{"body":"New card\n\n## Context\nd","epic":`+jsonNumber(epicID)+`}`)
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

func mergeURL(id int64, repo string) string {
	return "/api/tasks/" + jsonNumber(id) + "/merge?repo=" + url.QueryEscape(repo)
}

func TestMergeTask(t *testing.T) {
	e := newEnv(t, nil)
	target := e.add(t, repoA, "# target", task.Todo)
	source := e.add(t, repoA, "# source\n\nmore", task.Backlog)
	doing := e.add(t, repoA, "# claimed", task.Doing)
	other := e.add(t, repoB, "# other repo", task.Todo)

	rec := e.do(t, "POST", mergeURL(target.ID, repoA), `{"source":`+jsonNumber(source.ID)+`}`)
	expectStatus(t, rec, 200)
	got := decode[map[string]any](t, rec)
	wantBody := "# target\n\n## Merged from #" + jsonNumber(source.ID) + ": source\n\nmore"
	if got["body"] != wantBody || got["title"] != "target" || got["status"] != "todo" {
		t.Errorf("merged = %+v, want body %q", got, wantBody)
	}

	rec = e.do(t, "POST", mergeURL(target.ID, repoA), `{"source":`+jsonNumber(doing.ID)+`}`)
	expectStatus(t, rec, 409)
	if got := decode[errorJSON](t, rec); got.Current != task.Doing {
		t.Errorf("conflict body = %+v, want current doing", got)
	}
	expectStatus(t, e.do(t, "POST", mergeURL(target.ID, repoA), `{"source":`+jsonNumber(other.ID)+`}`), 404)
	expectStatus(t, e.do(t, "POST", mergeURL(target.ID, repoA), `{"source":`+jsonNumber(target.ID)+`}`), 400)
	expectStatus(t, e.do(t, "POST", mergeURL(target.ID, repoA), `{}`), 400)
	expectStatus(t, e.do(t, "POST", mergeURL(target.ID, repoA), `{"src":1}`), 400)
}

func TestCreateAndPatchEpicsAndPriorities(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.do(t, "POST", tasksURL(repoA), `{"body":"# Auth rewrite","kind":"epic","priority":"high"}`)
	expectStatus(t, rec, 201)
	epic := decode[map[string]any](t, rec)
	if epic["kind"] != "epic" || epic["priority"] != "high" || epic["epicId"] != nil {
		t.Errorf("epic = %+v", epic)
	}
	epicID := jsonNumber(int64(epic["id"].(float64)))

	rec = e.do(t, "POST", tasksURL(repoA), `{"body":"# Login","epic":`+epicID+`}`)
	expectStatus(t, rec, 201)
	child := decode[map[string]any](t, rec)
	if child["kind"] != "task" || child["priority"] != "normal" || child["epicId"] != epic["id"] {
		t.Errorf("child = %+v", child)
	}
	childID := int64(child["id"].(float64))

	expectStatus(t, e.do(t, "PATCH", taskURL(childID, repoA), `{"priority":"low","epic":0}`), 400)
	rec = e.do(t, "PATCH", taskURL(childID, repoA), `{"priority":"low"}`)
	expectStatus(t, rec, 200)
	if got := decode[map[string]any](t, rec); got["priority"] != "low" || got["epicId"] != epic["id"] {
		t.Errorf("patched = %+v", got)
	}

	expectStatus(t, e.do(t, "POST", tasksURL(repoA), `{"body":"# x","kind":"story"}`), 400)
	expectStatus(t, e.do(t, "POST", tasksURL(repoA), `{"body":"# x","priority":"urgent"}`), 400)
	expectStatus(t, e.do(t, "POST", tasksURL(repoA), `{"body":"# x","epic":`+jsonNumber(childID)+`}`), 400)
	expectStatus(t, e.do(t, "POST", tasksURL(repoB), `{"body":"# x","epic":`+epicID+`}`), 404)
	expectStatus(t, e.do(t, "PATCH", taskURL(childID, repoA), `{"priority":"urgent"}`), 400)
}

func TestListTasksIncludesPrioritiesInOrder(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.do(t, "GET", tasksURL(repoA), "")
	expectStatus(t, rec, 200)
	var got struct {
		Priorities []string `json:"priorities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Priorities, ",") != "low,normal,high" {
		t.Errorf("priorities = %v", got.Priorities)
	}
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
		{"POST", mergeURL(tk.ID, repoA)},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(`{"body":"x"}`))
		req.Host = "127.0.0.1:7420"
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		expectStatus(t, rec, 415)
	}
	if got, _ := e.store.List(context.Background(), repoA, store.Filter{}); len(got) != 2 || got[0].Title() != "card" {
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

func TestListTasksOfEmptyRepoIsEmptyArray(t *testing.T) {
	e := newEnv(t, nil)
	rec := e.do(t, "GET", tasksURL(repoB), "")
	expectStatus(t, rec, 200)
	// The board calls tasks.filter; null would break it.
	if !strings.Contains(rec.Body.String(), `"tasks":[]`) {
		t.Errorf("body = %s, want an empty tasks array", rec.Body.String())
	}
}

func TestRejectsOversizedBody(t *testing.T) {
	e := newEnv(t, nil)
	big := `{"body":"# big ` + strings.Repeat("a", maxBody) + `"}`
	expectStatus(t, e.do(t, "POST", tasksURL(repoA), big), 400)
	if repos, _ := e.store.Repos(context.Background()); len(repos) != 0 {
		t.Errorf("oversized body was stored: %v", repos)
	}
}

func TestInternalErrorHidesDetails(t *testing.T) {
	e := newEnv(t, nil)
	e.store.Close() // every query now fails inside database/sql
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	rec := e.do(t, "GET", "/api/repos", "")
	expectStatus(t, rec, 500)
	if got := decode[errorJSON](t, rec); got.Error != "internal error" {
		t.Errorf("error = %q, want the generic message", got.Error)
	}
}

func TestServesIndexForBoardPaths(t *testing.T) {
	e := newEnv(t, fstest.MapFS{
		"index.html":    {Data: []byte("<h1>board</h1>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
	for _, path := range []string{"/", "/Users/ozan.yildiz/projects/agent-todo", "/work/a%20b"} {
		rec := e.do(t, "GET", path, "")
		expectStatus(t, rec, 200)
		if rec.Body.String() != "<h1>board</h1>" {
			t.Errorf("GET %s = %q, want index.html", path, rec.Body.String())
		}
	}
	rec := e.do(t, "GET", "/assets/app.js", "")
	expectStatus(t, rec, 200)
	if rec.Body.String() != "console.log(1)" {
		t.Errorf("asset body = %q", rec.Body.String())
	}
	// Missing assets and API routes must not turn into the HTML page.
	for _, path := range []string{"/assets/missing.js", "/api/nope"} {
		if rec := e.do(t, "GET", path, ""); rec.Code != 404 {
			t.Errorf("GET %s = %d %q, want 404", path, rec.Code, rec.Body.String())
		}
	}
}

func TestTaskRequiresEpicOverHTTP(t *testing.T) {
	e := newEnv(t, nil)
	expectStatus(t, e.do(t, "POST", tasksURL(repoA), `{"body":"# Missing parent"}`), 400)
	epicID := e.epic(t, repoA)
	child := e.add(t, repoA, "# Child", task.Todo)
	expectStatus(t, e.do(t, "DELETE", taskURL(epicID, repoA), ""), 400)
	expectStatus(t, e.do(t, "DELETE", taskURL(child.ID, repoA), ""), 204)
	expectStatus(t, e.do(t, "DELETE", taskURL(epicID, repoA), ""), 204)
}
