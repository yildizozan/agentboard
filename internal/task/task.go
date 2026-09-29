// Package task holds the domain rules shared by the CLI, MCP server and HTTP API.
package task

import (
	"encoding/json"
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

// Task is one card on a repo's board. Its content is a single Markdown body whose
// first line is the title.
type Task struct {
	ID        int64     `json:"id"`
	Repo      string    `json:"repo"`
	Body      string    `json:"body"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Title returns the card title derived from the body.
func (t Task) Title() string { return TitleOf(t.Body) }

// MarshalJSON adds the derived title, so clients do not reimplement TitleOf.
func (t Task) MarshalJSON() ([]byte, error) {
	type plain Task // drops the method set, so json.Marshal does not recurse
	return json.Marshal(struct {
		plain
		Title string `json:"title"`
	}{plain(t), t.Title()})
}

// String returns the compact line used by the CLI and MCP output.
func (t Task) String() string {
	return fmt.Sprintf("#%d [%s] %s", t.ID, t.Status, t.Title())
}

// Detail returns the compact line followed by the full body, for reading one task.
func (t Task) Detail() string {
	return t.String() + "\n\n" + t.Body
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

// ErrNoTitle is returned for a body that does not start with a level-1 heading.
var ErrNoTitle error = invalidError{msg: `body must start with a level-1 heading, e.g. "# Fix login bug"`}

// TitleOf returns the text of the body's first line without its "#" heading marker.
func TitleOf(body string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(first), "#"))
}

// ValidateBody returns the trimmed body with its first line normalized to "# <title>".
// A plain first line becomes the heading; a deeper heading ("## ...") or a blank title is rejected.
func ValidateBody(body string) (string, error) {
	first, rest, hasRest := strings.Cut(strings.TrimSpace(body), "\n")
	first = strings.TrimSpace(first)
	if strings.HasPrefix(first, "##") {
		return "", ErrNoTitle
	}
	title := strings.TrimSpace(strings.TrimPrefix(first, "#"))
	if title == "" {
		return "", ErrNoTitle
	}
	body = "# " + title
	if hasRest {
		body += "\n" + rest
	}
	return body, nil
}

// Patch describes a change to a task; nil fields stay unchanged.
// When From is set the change applies only if the task is currently in From (compare-and-swap).
type Patch struct {
	Body   *string `json:"body,omitempty"`
	Status *Status `json:"status,omitempty"`
	From   *Status `json:"from,omitempty"`
}

// Validate checks p and returns it with the body normalized.
func (p Patch) Validate() (Patch, error) {
	if p.Body == nil && p.Status == nil {
		return Patch{}, Invalidf("nothing to change: set body or status")
	}
	if p.From != nil && p.Status == nil {
		return Patch{}, Invalidf("from requires status")
	}
	if p.Body != nil {
		body, err := ValidateBody(*p.Body)
		if err != nil {
			return Patch{}, err
		}
		p.Body = &body
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
