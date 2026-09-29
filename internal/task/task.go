// Package task holds the domain rules shared by the CLI, MCP server and HTTP API.
package task

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Status is a board column.
type Status string

const (
	Backlog Status = "backlog"
	Todo    Status = "todo"
	Doing   Status = "doing"
	Done    Status = "done"
)

// statuses is the single source of valid statuses and their display order.
var statuses = []Status{Backlog, Todo, Doing, Done}

// Statuses returns all statuses in board order.
func Statuses() []Status { return slices.Clone(statuses) }

// ActiveStatuses returns the statuses shown when no filter is given.
func ActiveStatuses() []Status { return []Status{Backlog, Todo, Doing} }

// ParseStatus converts s to a Status or reports the valid values.
func ParseStatus(s string) (Status, error) {
	if slices.Contains(statuses, Status(s)) {
		return Status(s), nil
	}
	names := make([]string, len(statuses))
	for i, st := range statuses {
		names[i] = string(st)
	}
	return "", fmt.Errorf("invalid status %q: must be one of %s", s, strings.Join(names, ", "))
}

// Task is one card on a repo's board.
type Task struct {
	ID          int64
	Repo        string
	Title       string
	Description string
	Status      Status
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// String returns the compact line used by the CLI and MCP output.
func (t Task) String() string {
	return fmt.Sprintf("#%d [%s] %s", t.ID, t.Status, t.Title)
}

// ErrEmptyTitle is returned for a blank title.
var ErrEmptyTitle = errors.New("title must not be empty")

// ValidateTitle returns the trimmed title or ErrEmptyTitle.
func ValidateTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ErrEmptyTitle
	}
	return title, nil
}

// ErrNotFound is returned when a task does not exist in the given repo.
var ErrNotFound = errors.New("task not found")

// ConflictError is returned when a compare-and-swap move finds the task in another status.
type ConflictError struct {
	ID      int64
	Current Status
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("task #%d is in %s", e.ID, e.Current)
}
