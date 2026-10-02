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
func ParseStatus(s string) (Status, error) { return parseOneOf("status", s, statuses) }

// Kind tells an ordinary task from an epic, which groups other tasks.
type Kind string

const (
	TaskKind Kind = "task"
	EpicKind Kind = "epic"
)

var kinds = []Kind{TaskKind, EpicKind}

// ParseKind converts s to a Kind or reports the valid values.
func ParseKind(s string) (Kind, error) { return parseOneOf("kind", s, kinds) }

// Priority orders the tasks of a column: high first, low last.
type Priority string

const (
	Low    Priority = "low"
	Normal Priority = "normal"
	High   Priority = "high"
)

// priorities is the single source of valid priorities, from lowest to highest.
var priorities = []Priority{Low, Normal, High}

// Priorities returns all priorities from lowest to highest.
func Priorities() []Priority { return slices.Clone(priorities) }

// ParsePriority converts s to a Priority or reports the valid values.
func ParsePriority(s string) (Priority, error) { return parseOneOf("priority", s, priorities) }

// Rank sorts priorities: 0 for high, larger for lower priorities.
func (p Priority) Rank() int { return len(priorities) - 1 - slices.Index(priorities, p) }

// parseOneOf returns s as a T when it is one of valid, or an error listing the valid values.
func parseOneOf[T ~string](what, s string, valid []T) (T, error) {
	if slices.Contains(valid, T(s)) {
		return T(s), nil
	}
	names := make([]string, len(valid))
	for i, v := range valid {
		names[i] = string(v)
	}
	return "", Invalidf("invalid %s %q: must be one of %s", what, s, strings.Join(names, ", "))
}

// Task is one card on a repo's board. Its content is a single Markdown body whose
// first line is the title.
type Task struct {
	ID        int64     `json:"id"`
	Repo      string    `json:"repo"`
	Body      string    `json:"body"`
	Status    Status    `json:"status"`
	Kind      Kind      `json:"kind"`
	Priority  Priority  `json:"priority"`
	EpicID    *int64    `json:"epicId"` // the epic this task belongs to; nil for an epic
	Revision  int64     `json:"revision"`
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

// String returns the compact line used by the CLI and MCP output, e.g.
// "#12 [doing] [high] Fix login bug (epic #3)". Normal priority is not shown.
func (t Task) String() string {
	line := fmt.Sprintf("#%d [%s]", t.ID, t.Status)
	if t.Kind == EpicKind {
		line += " [epic]"
	}
	if t.Priority != Normal && t.Priority != "" {
		line += fmt.Sprintf(" [%s]", t.Priority)
	}
	line += " " + t.Title()
	if t.EpicID != nil {
		line += fmt.Sprintf(" (epic #%d)", *t.EpicID)
	}
	return line
}

// Detail returns the compact line, revision and full body, for reading one task.
func (t Task) Detail() string {
	return fmt.Sprintf("%s\nRevision: %d\n\n%s", t.String(), t.Revision, t.Body)
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

// Draft is a task to be added. Empty Status, Kind and Priority take their defaults.
type Draft struct {
	Body     string
	Status   Status
	Kind     Kind
	Priority Priority
	EpicID   *int64
}

// Validate returns d with the body normalized and defaults filled in: backlog, task, normal.
// Whether EpicID names an epic of the same board is checked by the store.
func (d Draft) Validate() (Draft, error) {
	body, err := ValidateBody(d.Body)
	if err != nil {
		return Draft{}, err
	}
	d.Body = body
	if d.Status, err = parseOr(d.Status, Backlog, ParseStatus); err != nil {
		return Draft{}, err
	}
	if d.Kind, err = parseOr(d.Kind, TaskKind, ParseKind); err != nil {
		return Draft{}, err
	}
	if d.Priority, err = parseOr(d.Priority, Normal, ParsePriority); err != nil {
		return Draft{}, err
	}
	if err := ValidateEpic(d.Kind, d.EpicID); err != nil {
		return Draft{}, err
	}
	return d, nil
}

// ErrEpicRequired is returned when a task has no parent epic.
var ErrEpicRequired error = invalidError{msg: "a task must belong to an epic"}

// ErrEpicHasTasks is returned when deleting an epic would leave its tasks without a parent.
var ErrEpicHasTasks error = invalidError{msg: "epic still has tasks: move or delete them first"}

// ValidateEpic checks the parent rule; the store checks existence and board membership.
func ValidateEpic(kind Kind, epicID *int64) error {
	if kind == EpicKind {
		if epicID != nil {
			return ErrNestedEpic
		}
		return nil
	}
	if epicID == nil || *epicID == 0 {
		return ErrEpicRequired
	}
	if *epicID < 0 {
		return Invalidf("invalid epic id %d", *epicID)
	}
	return nil
}

// ErrNestedEpic is returned when an epic would be put into another epic.
var ErrNestedEpic error = invalidError{msg: "an epic cannot belong to another epic"}

// parseOr returns def for an empty v, and otherwise v checked by parse.
func parseOr[T ~string](v, def T, parse func(string) (T, error)) (T, error) {
	if v == "" {
		return def, nil
	}
	return parse(string(v))
}

// Patch describes a change to a task; nil fields stay unchanged.
// When From is set the change applies only if the task is currently in From (compare-and-swap).
// Epic moves the task to another epic; it must be a positive id.
// ExpectedRevision rejects stale changes when set; nil keeps unconditional updates compatible.
type Patch struct {
	Body             *string   `json:"body,omitempty"`
	Status           *Status   `json:"status,omitempty"`
	From             *Status   `json:"from,omitempty"`
	Priority         *Priority `json:"priority,omitempty"`
	Epic             *int64    `json:"epic,omitempty"`
	ExpectedRevision *int64    `json:"expectedRevision,omitempty"`
}

// Validate checks p and returns it with the body normalized.
func (p Patch) Validate() (Patch, error) {
	if p.Body == nil && p.Status == nil && p.Priority == nil && p.Epic == nil {
		return Patch{}, Invalidf("nothing to change: set body, status, priority or epic")
	}
	if p.ExpectedRevision != nil && *p.ExpectedRevision < 1 {
		return Patch{}, Invalidf("expected revision must be positive")
	}
	if p.Priority != nil {
		if _, err := ParsePriority(string(*p.Priority)); err != nil {
			return Patch{}, err
		}
	}
	if p.Epic != nil && *p.Epic == 0 {
		return Patch{}, ErrEpicRequired
	}
	if p.Epic != nil && *p.Epic < 0 {
		return Patch{}, Invalidf("invalid epic id %d", *p.Epic)
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

// RevisionConflictError means a task changed since the caller read it.
type RevisionConflictError struct {
	ID       int64
	Expected int64
	Current  int64
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("task #%d changed: expected revision %d, current revision %d; reload before saving", e.ID, e.Expected, e.Current)
}
