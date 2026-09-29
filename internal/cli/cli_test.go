package cli

import (
	"bytes"
	"strings"
	"testing"
)

// run executes the root command with args and returns its combined output.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestHelpShowsRepoFlag(t *testing.T) {
	out, err := run(t, "--help")
	if err != nil {
		t.Fatalf("--help error: %v", err)
	}
	for _, want := range []string{"agentboard", "--repo"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q:\n%s", want, out)
		}
	}
}
