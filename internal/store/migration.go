package store

import (
	"context"
	"database/sql"
	"fmt"
)

// v0.1 used user_version 1; v0.2 left it at zero. Inspect the table shape so
// both released layouts can enter the versioned schema without losing data.
const schemaVersion = 2

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
	if version > schemaVersion {
		return fmt.Errorf("database schema version %d is newer than this binary supports (%d)", version, schemaVersion)
	}
	columns, err := taskTableColumns(ctx, tx)
	if err != nil {
		return err
	}
	if len(columns) != 0 {
		if err := upgradeTaskTable(ctx, tx, columns); err != nil {
			return fmt.Errorf("upgrade tasks: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("write schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

func taskTableColumns(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info('tasks')`)
	if err != nil {
		return nil, fmt.Errorf("read task columns: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read task column: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read task columns: %w", err)
	}
	return columns, nil
}

func upgradeTaskTable(ctx context.Context, tx *sql.Tx, columns map[string]bool) error {
	for _, name := range []string{"id", "repo", "status", "created_at", "updated_at"} {
		if !columns[name] {
			return fmt.Errorf("unsupported tasks schema: missing %s", name)
		}
	}
	if !columns["body"] {
		if !columns["title"] || !columns["description"] {
			return fmt.Errorf("unsupported tasks schema: missing body or legacy title and description")
		}
		// ALTER preserves the table, IDs and sqlite_sequence high-water mark.
		// Keep all title/description text, including Markdown whitespace.
		if _, err := tx.ExecContext(ctx, `ALTER TABLE tasks RENAME COLUMN title TO body;
			UPDATE tasks SET body = '# ' || body || CASE WHEN description = '' THEN '' ELSE char(10) || char(10) || description END;
			ALTER TABLE tasks DROP COLUMN description;`); err != nil {
			return fmt.Errorf("convert legacy task bodies: %w", err)
		}
	}
	for _, column := range []struct{ name, definition string }{
		{"kind", "TEXT NOT NULL DEFAULT 'task'"},
		{"priority", "TEXT NOT NULL DEFAULT 'normal'"},
		{"epic_id", "INTEGER REFERENCES tasks(id) ON DELETE SET NULL"},
		{"revision", "INTEGER NOT NULL DEFAULT 1"},
	} {
		if columns[column.name] {
			continue
		}
		// The identifiers and definitions are fixed above, never caller input.
		if _, err := tx.ExecContext(ctx, "ALTER TABLE tasks ADD COLUMN "+column.name+" "+column.definition); err != nil {
			return fmt.Errorf("add %s: %w", column.name, err)
		}
	}
	return nil
}
