package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/yildizozan/agentboard/internal/task"
)

// These are released schemas, deliberately independent of the current schema.
const schemaV01Fixture = `CREATE TABLE tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  repo TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_tasks_repo_status ON tasks(repo, status);
PRAGMA user_version = 1;`

const schemaV02Fixture = `CREATE TABLE tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  repo TEXT NOT NULL,
  body TEXT NOT NULL,
  status TEXT NOT NULL,
  kind TEXT NOT NULL,
  priority TEXT NOT NULL,
  epic_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_tasks_repo_status ON tasks(repo, status);
CREATE INDEX idx_tasks_epic ON tasks(epic_id);`

func fixtureDatabase(t *testing.T, schema string) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agentboard.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func TestOpenMigratesReleasedV01WithoutLosingTasks(t *testing.T) {
	db, path := fixtureDatabase(t, schemaV01Fixture)
	if _, err := db.Exec(`INSERT INTO tasks (id,repo,title,description,status,created_at,updated_at) VALUES
		(7,'/r','Original title','  details with spaces  ','doing',101,202),
		(9,'/other','Title only','','done',303,404),
		(50,'/r','Deleted task','','todo',1,1);
		DELETE FROM tasks WHERE id=50;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	ctx := context.Background()
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open released v0.1.0 database: %v", err)
		}
		got, err := s.Get(ctx, "/r", 7)
		if err != nil || got.Body != "# Original title\n\n  details with spaces  " || got.Status != task.Doing ||
			got.Kind != task.TaskKind || got.Priority != task.Normal || got.EpicID != nil || got.Revision != 1 ||
			got.CreatedAt.Unix() != 101 || got.UpdatedAt.Unix() != 202 {
			t.Fatalf("legacy task changed during migration: %+v, %v", got, err)
		}
		other, err := s.Get(ctx, "/other", 9)
		if err != nil || other.Body != "# Title only" || other.Status != task.Done || other.CreatedAt.Unix() != 303 || other.UpdatedAt.Unix() != 404 {
			t.Fatalf("other board's task changed: %+v, %v", other, err)
		}
		s.Close()
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Update(ctx, "/r", 7, task.Patch{Status: ptr(task.Done)}); !errors.Is(err, task.ErrEpicRequired) {
		t.Fatalf("legacy orphan update error = %v, want epic required", err)
	}
	if err := s.Delete(ctx, "/other", 9); err != nil {
		t.Fatalf("legacy orphan cannot be deleted: %v", err)
	}
	epic, err := s.Add(ctx, "/r", task.Draft{Body: "# Parent", Kind: task.EpicKind})
	if err != nil || epic.ID != 51 {
		t.Fatalf("insert after migration = %+v, %v; want ID 51 without reusing deleted IDs", epic, err)
	}
	got, err := s.Update(ctx, "/r", 7, task.Patch{Epic: &epic.ID})
	if err != nil || !sameEpic(got.EpicID, &epic.ID) || got.Revision != 2 {
		t.Fatalf("assign migrated task to epic = %+v, %v", got, err)
	}
}

func TestOpenMigratesCurrentSchemaConcurrently(t *testing.T) {
	db, path := fixtureDatabase(t, schemaV02Fixture)
	if _, err := db.Exec(`INSERT INTO tasks (id,repo,body,status,kind,priority,epic_id,created_at,updated_at) VALUES
		(2,'/r','# Epic','doing','epic','high',NULL,101,202),
		(7,'/r','# Child','todo','task','low',2,303,404);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	const count = 4
	stores := make(chan *Store, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			s, err := Open(path)
			if err != nil {
				errs <- err
				return
			}
			stores <- s
		})
	}
	wg.Wait()
	close(stores)
	close(errs)
	for err := range errs {
		t.Errorf("concurrent migration failed: %v", err)
	}
	for s := range stores {
		got, err := s.Get(context.Background(), "/r", 7)
		if err != nil || got.Body != "# Child" || got.Status != task.Todo || got.Kind != task.TaskKind ||
			got.Priority != task.Low || !sameEpic(got.EpicID, ptr(int64(2))) || got.Revision != 1 ||
			got.CreatedAt.Unix() != 303 || got.UpdatedAt.Unix() != 404 {
			t.Errorf("current task changed during upgrade: %+v, %v", got, err)
		}
		s.Close()
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	db, path := fixtureDatabase(t, schemaV02Fixture)
	if _, err := db.Exec(`PRAGMA user_version=999`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if s != nil {
		s.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Open newer schema = %v, want version error", err)
	}
}

func TestFailedMigrationRollsBackLegacyBodyConversion(t *testing.T) {
	db, path := fixtureDatabase(t, schemaV01Fixture)
	// A schema-owned index blocks dropping this legacy column. The conversion
	// must roll back entirely rather than leave only some tasks or columns changed.
	if _, err := db.Exec(`INSERT INTO tasks (id,repo,title,description,status,created_at,updated_at)
		VALUES (7,'/r','Original','Description','todo',101,202);
		CREATE INDEX custom_description ON tasks(description);`); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if s != nil {
		s.Close()
	}
	if err == nil {
		t.Fatal("migration with incompatible custom index succeeded")
	}
	var title, description string
	var version, sequence int
	if err := db.QueryRow(`SELECT title,description FROM tasks WHERE id=7`).Scan(&title, &description); err != nil {
		t.Fatalf("failed migration changed legacy columns: %v", err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='tasks'`).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	if title != "Original" || description != "Description" || version != 1 || sequence != 7 {
		t.Fatalf("failed migration changed data: %q %q, version=%d sequence=%d", title, description, version, sequence)
	}
}

func TestOpenPreservesLiteralDatabasePaths(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"question?mark.db", "hash#mark.db", "percent%23.db", "çalışma 東京.db"} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.Contains(name, "?") {
				t.Skip("Windows filenames cannot contain question marks")
			}
			path := filepath.Join(base, name)
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			added, err := s.Add(context.Background(), "/r", task.Draft{Body: "# Persisted", Kind: task.EpicKind})
			s.Close()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("database was not created at literal path %q: %v", path, err)
			}
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if got, err := s.Get(context.Background(), "/r", added.ID); err != nil || got.Body != "# Persisted" {
				t.Fatalf("task did not survive reopening literal path: %+v, %v", got, err)
			}
		})
	}
}
