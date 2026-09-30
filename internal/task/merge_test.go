package task

import "testing"

func TestMergeBody(t *testing.T) {
	target := Task{ID: 3, Body: "# Fix login\n\n## Context\nA"}
	cases := []struct {
		name   string
		source Task
		want   string
	}{
		{
			name:   "sections move one level down below a merged-from heading",
			source: Task{ID: 7, Body: "# Login broken\n\n## Context\nB\n### Deep\nC"},
			want:   "# Fix login\n\n## Context\nA\n\n## Merged from #7: Login broken\n\n### Context\nB\n#### Deep\nC",
		},
		{
			name:   "code blocks keep their lines",
			source: Task{ID: 8, Body: "# Log\n\n```sh\n# not a heading\n```\n~~~\n## still code\n~~~\n## Real"},
			want:   "# Fix login\n\n## Context\nA\n\n## Merged from #8: Log\n\n```sh\n# not a heading\n```\n~~~\n## still code\n~~~\n### Real",
		},
		{
			name:   "a title-only source adds only the merged-from heading",
			source: Task{ID: 9, Body: "# Duplicate"},
			want:   "# Fix login\n\n## Context\nA\n\n## Merged from #9: Duplicate",
		},
		{
			name:   "level six stays level six and non-headings stay as they are",
			source: Task{ID: 10, Body: "# Deep\n\n###### Six\n#hashtag\n    # indented code"},
			want:   "# Fix login\n\n## Context\nA\n\n## Merged from #10: Deep\n\n###### Six\n#hashtag\n    # indented code",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeBody(target, tc.source)
			if got != tc.want {
				t.Errorf("MergeBody =\n%q\nwant\n%q", got, tc.want)
			}
			if TitleOf(got) != "Fix login" {
				t.Errorf("merged title = %q, want the target title", TitleOf(got))
			}
		})
	}
}
