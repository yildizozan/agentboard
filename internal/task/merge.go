package task

import (
	"fmt"
	"regexp"
	"strings"
)

// heading matches a Markdown ATX heading: up to three spaces, 1-6 "#", then a space or the end.
var heading = regexp.MustCompile(`^ {0,3}(#{1,6})([ \t]|$)`)

// MergeBody appends source to target's body as a "## Merged from #<id>: <title>" section.
// The source's own title line is dropped and its headings move one level down, so the
// merged body keeps target's title and no content is lost.
func MergeBody(target, source Task) string {
	merged := fmt.Sprintf("%s\n\n## Merged from #%d: %s", target.Body, source.ID, source.Title())
	_, rest, _ := strings.Cut(source.Body, "\n")
	if rest = strings.TrimSpace(rest); rest != "" {
		merged += "\n\n" + demoteHeadings(rest)
	}
	return merged
}

// demoteHeadings adds one "#" to every heading below level six, leaving fenced code alone.
func demoteHeadings(markdown string) string {
	lines := strings.Split(markdown, "\n")
	fence := ""
	for i, line := range lines {
		if marker := fenceMarker(line); marker != "" {
			switch fence {
			case "":
				fence = marker
			case marker:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if m := heading.FindStringSubmatchIndex(line); m != nil && m[3]-m[2] < 6 {
			lines[i] = line[:m[2]] + "#" + line[m[2]:]
		}
	}
	return strings.Join(lines, "\n")
}

// fenceMarker returns "```" or "~~~" when line opens or closes a fenced code block.
func fenceMarker(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return ""
	}
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			return marker
		}
	}
	return ""
}
