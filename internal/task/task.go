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
	return "", Invalidf("invalid status %q: must be one of %s", s, strings.Join(names, ", "))
}

// Task is one card on a repo's board.
type Task struct {
	ID          int64     `json:"id"`
	Repo        string    `json:"repo"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// String returns the compact line used by the CLI and MCP output.
func (t Task) String() string {
	return fmt.Sprintf("#%d [%s] %s", t.ID, t.Status, t.Title)
}

// ErrInvalid matches every input validation error (errors.Is), so callers can map them to one response.
var ErrInvalid = errors.New("invalid input")

type invalidError struct{ msg string }

func (e invalidError) Error() string        { return e.msg }
func (e invalidError) Is(target error) bool { return target == ErrInvalid }

// Invalidf returns a validation error that matches ErrInvalid.
func Invalidf(format string, a ...any) error {
	return invalidError{msg: fmt.Sprintf(format, a...)}
}

// ErrEmptyTitle is returned for a blank title.
var ErrEmptyTitle error = invalidError{msg: "title must not be empty"}

// ValidateTitle returns the trimmed title or ErrEmptyTitle.
func ValidateTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ErrEmptyTitle
	}
	return title, nil
}

// Patch describes a change to a task; nil fields stay unchanged.
// When From is set the change applies only if the task is currently in From (compare-and-swap).
type Patch struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *Status `json:"status,omitempty"`
	From        *Status `json:"from,omitempty"`
}

// Validate checks p and returns it with the title normalized.
func (p Patch) Validate() (Patch, error) {
	if p.Title == nil && p.Description == nil && p.Status == nil {
		return Patch{}, Invalidf("nothing to change: set title, description or status")
	}
	if p.From != nil && p.Status == nil {
		return Patch{}, Invalidf("from requires status")
	}
	if p.Title != nil {
		title, err := ValidateTitle(*p.Title)
		if err != nil {
			return Patch{}, err
		}
		p.Title = &title
	}
	for _, st := range []*Status{p.Status, p.From} {
		if st == nil {
			continue
		}
		if _, err := ParseStatus(string(*st)); err != nil {
			return Patch{}, err
		}
	}
	return p, nil
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
