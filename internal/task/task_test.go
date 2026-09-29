package task

import (
	"encoding/json"
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

func TestValidateBody(t *testing.T) {
	cases := map[string]string{
		"  # Fix login bug \n\n## Context\n- token\n": "# Fix login bug\n\n## Context\n- token",
		"Fix login bug":          "# Fix login bug",
		"#Fix login bug\r\nmore": "# Fix login bug\nmore",
		"\n\n#   Spaced   ":      "# Spaced",
	}
	for in, want := range cases {
		got, err := ValidateBody(in)
		if err != nil {
			t.Fatalf("ValidateBody(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("ValidateBody(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateBodyRejects(t *testing.T) {
	for _, s := range []string{"", "   ", "\t\n", "#", "#  \nbody", "## Context\n- item"} {
		if _, err := ValidateBody(s); !errors.Is(err, ErrNoTitle) {
			t.Errorf("ValidateBody(%q) error = %v, want ErrNoTitle", s, err)
		}
	}
}

func TestTitleOf(t *testing.T) {
	cases := map[string]string{
		"# Fix login bug\n\n## Context": "Fix login bug",
		"# #12 keeps inner hash":        "#12 keeps inner hash",
		"":                              "",
	}
	for in, want := range cases {
		if got := TitleOf(in); got != want {
			t.Errorf("TitleOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTaskString(t *testing.T) {
	tk := Task{ID: 12, Status: Doing, Body: "# Fix login bug\n\n## Context"}
	if got, want := tk.String(), "#12 [doing] Fix login bug"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := tk.Detail(), "#12 [doing] Fix login bug\n\n# Fix login bug\n\n## Context"; got != want {
		t.Errorf("Detail() = %q, want %q", got, want)
	}
}

func TestTaskJSONIncludesTitle(t *testing.T) {
	data, err := json.Marshal(Task{ID: 1, Body: "# Title\n\ntext", Status: Todo})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["title"] != "Title" || got["body"] != "# Title\n\ntext" || got["status"] != "todo" {
		t.Errorf("JSON = %s", data)
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
	p, err := Patch{Body: ptr("  New title "), Status: ptr(Doing), From: ptr(Todo)}.Validate()
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if *p.Body != "# New title" {
		t.Errorf("body not normalized: %q", *p.Body)
	}
}

func TestPatchValidateRejects(t *testing.T) {
	cases := map[string]Patch{
		"empty":          {},
		"from only":      {From: ptr(Todo)},
		"blank body":     {Body: ptr("  ")},
		"invalid status": {Status: ptr(Status("later"))},
		"invalid from":   {Status: ptr(Doing), From: ptr(Status("later"))},
	}
	for name, p := range cases {
		if _, err := p.Validate(); err == nil {
			t.Errorf("%s: Validate returned no error", name)
		}
	}
}

func TestValidationErrorsMatchErrInvalid(t *testing.T) {
	_, statusErr := ParseStatus("later")
	_, titleErr := ValidateBody(" ")
	_, patchErr := Patch{}.Validate()
	for _, err := range []error{statusErr, titleErr, patchErr, Invalidf("x")} {
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%v does not match ErrInvalid", err)
		}
	}
	if errors.Is(ErrNotFound, ErrInvalid) {
		t.Error("ErrNotFound must not match ErrInvalid")
	}
}
