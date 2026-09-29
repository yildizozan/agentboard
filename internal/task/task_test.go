package task

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseStatus(t *testing.T) {
	for _, s := range []string{"backlog", "todo", "doing", "done"} {
		got, err := ParseStatus(s)
		if err != nil {
			t.Fatalf("ParseStatus(%q) error: %v", s, err)
		}
		if string(got) != s {
			t.Errorf("ParseStatus(%q) = %q", s, got)
		}
	}
}

func TestParseStatusRejectsUnknown(t *testing.T) {
	for _, s := range []string{"", "Doing", "in-progress", " todo"} {
		_, err := ParseStatus(s)
		if err == nil {
			t.Fatalf("ParseStatus(%q) returned no error", s)
		}
		if !strings.Contains(err.Error(), "backlog, todo, doing, done") {
			t.Errorf("error %q does not list valid statuses", err)
		}
	}
}

func TestStatusesOrder(t *testing.T) {
	want := []Status{Backlog, Todo, Doing, Done}
	if got := Statuses(); !slices.Equal(got, want) {
		t.Errorf("Statuses() = %v, want %v", got, want)
	}
	if got := ActiveStatuses(); !slices.Equal(got, want[:3]) {
		t.Errorf("ActiveStatuses() = %v, want %v", got, want[:3])
	}
}

func TestStatusesReturnsCopy(t *testing.T) {
	Statuses()[0] = "broken"
	if Statuses()[0] != Backlog {
		t.Error("Statuses() exposes internal slice")
	}
}

func TestValidateTitle(t *testing.T) {
	got, err := ValidateTitle("  Fix login bug \n")
	if err != nil {
		t.Fatalf("ValidateTitle error: %v", err)
	}
	if got != "Fix login bug" {
		t.Errorf("ValidateTitle = %q, want trimmed title", got)
	}
}

func TestValidateTitleRejectsBlank(t *testing.T) {
	for _, s := range []string{"", "   ", "\t\n"} {
		if _, err := ValidateTitle(s); !errors.Is(err, ErrEmptyTitle) {
			t.Errorf("ValidateTitle(%q) error = %v, want ErrEmptyTitle", s, err)
		}
	}
}

func TestTaskString(t *testing.T) {
	tk := Task{ID: 12, Status: Doing, Title: "Fix login bug"}
	if got, want := tk.String(), "#12 [doing] Fix login bug"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestConflictError(t *testing.T) {
	var err error = &ConflictError{ID: 3, Current: Doing}
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Current != Doing {
		t.Fatalf("errors.As failed for %v", err)
	}
	if !strings.Contains(err.Error(), "doing") {
		t.Errorf("error %q does not mention current status", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestPatchValidate(t *testing.T) {
	p, err := Patch{Title: ptr("  New title "), Status: ptr(Doing), From: ptr(Todo)}.Validate()
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if *p.Title != "New title" {
		t.Errorf("title not trimmed: %q", *p.Title)
	}
}

func TestPatchValidateRejects(t *testing.T) {
	cases := map[string]Patch{
		"empty":          {},
		"from only":      {From: ptr(Todo)},
		"blank title":    {Title: ptr("  ")},
		"invalid status": {Status: ptr(Status("later"))},
		"invalid from":   {Status: ptr(Doing), From: ptr(Status("later"))},
	}
	for name, p := range cases {
		if _, err := p.Validate(); err == nil {
			t.Errorf("%s: Validate returned no error", name)
		}
	}
}
