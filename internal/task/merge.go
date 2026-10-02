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
	if rest = trimBlankLines(rest); rest != "" {
		merged += "\n\n" + demoteHeadings(rest)
	}
	return merged
}

// trimBlankLines removes section padding without stripping indentation or code spaces.
func trimBlankLines(markdown string) string {
	lines := strings.Split(markdown, "\n")
	for len(lines) > 0 && strings.Trim(lines[0], " \t\r") == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.Trim(lines[len(lines)-1], " \t\r") == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// demoteHeadings adds one "#" to every heading below level six, leaving fenced code alone.
func demoteHeadings(markdown string) string {
	lines := strings.Split(markdown, "\n")
	fence := ""
	for i, line := range lines {
		marker, rest := fenceMarker(line)
		if fence != "" {
			if len(marker) >= len(fence) && marker[0] == fence[0] && strings.Trim(rest, " \t\r") == "" {
				fence = ""
			}
			continue
		}
		if marker != "" && (marker[0] == '~' || !strings.Contains(rest, "`")) {
			fence = marker
			continue
		}
		if m := heading.FindStringSubmatchIndex(line); m != nil && m[3]-m[2] < 6 {
			lines[i] = line[:m[2]] + "#" + line[m[2]:]
		}
	}
	return strings.Join(lines, "\n")
}

// fenceMarker returns the full fence run and its suffix, if indented at most three spaces.
func fenceMarker(line string) (string, string) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return "", ""
	}
	n := 1
	for n < len(trimmed) && trimmed[n] == trimmed[0] {
		n++
	}
	if n < 3 {
		return "", ""
	}
	return trimmed[:n], trimmed[n:]
}

// MergedLine reports a merge in the CLI and MCP output: "merged #7 into #3" and the target's line.
func MergedLine(sourceID int64, target Task) string {
	return fmt.Sprintf("merged #%d into #%d\n%s", sourceID, target.ID, target)
}
