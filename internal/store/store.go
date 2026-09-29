// Package store persists tasks in a single SQLite file shared by all agentboard processes.
package store

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/yildizozan/agentboard/internal/task"
)

// dsnParams make concurrent writers from several processes safe:
// busy_timeout waits for locks instead of failing, and immediate transactions take
// the write lock up front so read-then-write steps cannot deadlock.
// WAL is not set here: it persists in the file and is enabled once by enableWAL.
const dsnParams = "?_pragma=busy_timeout(5000)&_txlock=immediate"

// lockWait bounds how long Open retries steps that SQLite does not cover with busy_timeout.
const lockWait = 5 * time.Second

//go:embed schema_v1.sql
var schemaV1 string

// migrations[i] upgrades the schema from user_version i to i+1.
var migrations = []string{schemaV1}

// Store is a handle to the task database.
type Store struct {
	db *sql.DB
}

// Open creates the parent directory if needed, opens the database and applies pending migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+path+dsnParams)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// One connection per process: writers inside a process queue in database/sql
	// instead of competing for the SQLite lock; only processes compete, via busy_timeout.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	ctx := context.Background()
	if err := s.enableWAL(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// enableWAL switches the file to WAL so readers do not block writers.
// Changing the journal mode ignores busy_timeout and fails at once while another
// process holds a lock (only on a fresh file), so it is retried until lockWait.
func (s *Store) enableWAL(ctx context.Context) error {
	deadline := time.Now().Add(lockWait)
	for {
		var mode string
		err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode)
		switch {
		case err == nil && mode == "wal":
			return nil
		case err == nil:
			return fmt.Errorf("enable WAL: journal mode is %q", mode)
		case !isBusy(err) || time.Now().After(deadline):
			return fmt.Errorf("enable WAL: %w", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func isBusy(err error) bool {
	var sqlErr *sqlite.Error
	return errors.As(err, &sqlErr) && sqlErr.Code()&0xff == sqlite3.SQLITE_BUSY
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this binary supports (%d)", version, len(migrations))
	}
	for v := version; v < len(migrations); v++ {
		if _, err := tx.ExecContext(ctx, migrations[v]); err != nil {
			return fmt.Errorf("migrate to version %d: %w", v+1, err)
		}
	}
	// PRAGMA does not accept bind parameters; the value is an int we control.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations))); err != nil {
		return fmt.Errorf("write schema version: %w", err)
	}
	return tx.Commit()
}

// Add validates and inserts a new task into repo.
func (s *Store) Add(ctx context.Context, repo, title, description string, status task.Status) (task.Task, error) {
	if repo == "" {
		return task.Task{}, errors.New("repo must not be empty")
	}
	title, err := task.ValidateTitle(title)
	if err != nil {
		return task.Task{}, err
	}
	if _, err := task.ParseStatus(string(status)); err != nil {
		return task.Task{}, err
	}

	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (repo, title, description, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		repo, title, description, string(status), now, now)
	if err != nil {
		return task.Task{}, fmt.Errorf("insert task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return task.Task{}, fmt.Errorf("read task id: %w", err)
	}
	return task.Task{
		ID:          id,
		Repo:        repo,
		Title:       title,
		Description: description,
		Status:      status,
		CreatedAt:   time.Unix(now, 0),
		UpdatedAt:   time.Unix(now, 0),
	}, nil
}

// List returns the tasks of repo whose status is in statuses (all statuses when empty),
// ordered by board column and then by id.
func (s *Store) List(ctx context.Context, repo string, statuses []task.Status) ([]task.Task, error) {
	query := `SELECT id, repo, title, description, status, created_at, updated_at FROM tasks WHERE repo = ?`
	args := []any{repo}
	if len(statuses) > 0 {
		query += " AND status IN (?" + strings.Repeat(", ?", len(statuses)-1) + ")"
		for _, st := range statuses {
			args = append(args, string(st))
		}
	}
	query += " ORDER BY id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []task.Task
	for rows.Next() {
		tk, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, tk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	order := task.Statuses()
	slices.SortStableFunc(tasks, func(a, b task.Task) int {
		return cmp.Compare(slices.Index(order, a.Status), slices.Index(order, b.Status))
	})
	return tasks, nil
}

func scanTask(rows *sql.Rows) (task.Task, error) {
	var (
		tk               task.Task
		status           string
		created, updated int64
	)
	if err := rows.Scan(&tk.ID, &tk.Repo, &tk.Title, &tk.Description, &status, &created, &updated); err != nil {
		return task.Task{}, fmt.Errorf("scan task: %w", err)
	}
	tk.Status = task.Status(status)
	tk.CreatedAt = time.Unix(created, 0)
	tk.UpdatedAt = time.Unix(updated, 0)
	return tk, nil
}
