package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register the MCP server with Codex and Claude Code",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if scope != "user" && scope != "project" {
				return fmt.Errorf("invalid scope %q: choose user or project", scope)
			}
			binary, err := os.Executable()
			if err != nil {
				return fmt.Errorf("find agentboard executable: %w", err)
			}
			binary, err = filepath.EvalSymlinks(binary)
			if err != nil {
				return fmt.Errorf("resolve agentboard executable: %w", err)
			}
			if scope == "project" {
				return installProject(cmd, binary)
			}
			return installUser(cmd, binary)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "user", "registration scope (user or project)")
	return cmd
}

func installUser(cmd *cobra.Command, binary string) error {
	for _, name := range []string{"codex", "claude"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("%s CLI is required for user scope: %w", name, err)
		}
	}
	for _, entry := range []struct {
		name string
		args []string
	}{
		{"codex", []string{"mcp", "add", "agentboard", "--", binary, "serve"}},
		{"claude", []string{"mcp", "add", "--scope", "user", "agentboard", "--", binary, "serve"}},
	} {
		output, err := exec.CommandContext(cmd.Context(), entry.name, entry.args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("register with %s: %w: %s", entry.name, err, strings.TrimSpace(string(output)))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Registered agentboard with %s (user)\n", entry.name)
	}
	return nil
}

type serverConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func installProject(cmd *cobra.Command, binary string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("find current directory: %w", err)
	}
	claudePath := filepath.Join(dir, ".mcp.json")
	codexPath := filepath.Join(dir, ".codex", "config.toml")
	claudeConfig, writeClaude, err := projectClaudeConfig(claudePath, binary)
	if err != nil {
		return err
	}
	codexConfig, writeCodex, err := projectCodexConfig(codexPath, binary)
	if err != nil {
		return err
	}
	if writeCodex {
		if err := os.MkdirAll(filepath.Dir(codexPath), 0o755); err != nil {
			return fmt.Errorf("create Codex config directory: %w", err)
		}
		if err := os.WriteFile(codexPath, codexConfig, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", codexPath, err)
		}
	}
	if writeClaude {
		if err := os.WriteFile(claudePath, claudeConfig, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", claudePath, err)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Registered agentboard with Codex and Claude Code (project: %s)\n", dir)
	return nil
}

func projectClaudeConfig(path, binary string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	config := make(map[string]json.RawMessage)
	if err == nil {
		if err := json.Unmarshal(data, &config); err != nil || config == nil {
			return nil, false, fmt.Errorf("invalid JSON in %s: %v", path, err)
		}
	}
	servers := make(map[string]json.RawMessage)
	if raw, ok := config["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil || servers == nil {
			return nil, false, fmt.Errorf("invalid mcpServers in %s: %v", path, err)
		}
	}
	wanted := serverConfig{Command: binary, Args: []string{"serve"}}
	if raw, ok := servers["agentboard"]; ok {
		var existing serverConfig
		if json.Unmarshal(raw, &existing) == nil && existing.Command == wanted.Command && len(existing.Args) == 1 && existing.Args[0] == "serve" {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("agentboard already configured differently in %s", path)
	}
	server, err := json.Marshal(wanted)
	if err != nil {
		return nil, false, err
	}
	servers["agentboard"] = server
	config["mcpServers"], err = json.Marshal(servers)
	if err != nil {
		return nil, false, err
	}
	result, err := json.MarshalIndent(config, "", "  ")
	return append(result, '\n'), true, err
}

func projectCodexConfig(path, binary string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	if err == nil {
		var config map[string]any
		if parseErr := toml.Unmarshal(data, &config); parseErr != nil {
			return nil, false, fmt.Errorf("invalid TOML in %s: %w", path, parseErr)
		}
		if servers, ok := config["mcp_servers"].(map[string]any); ok {
			if existing, ok := servers["agentboard"]; ok {
				entry, ok := existing.(map[string]any)
				if ok && entry["command"] == binary {
					if args, ok := entry["args"].([]any); ok && len(args) == 1 && args[0] == "serve" {
						return nil, false, nil
					}
				}
				return nil, false, fmt.Errorf("agentboard already configured differently in %s", path)
			}
		} else if config["mcp_servers"] != nil {
			return nil, false, fmt.Errorf("invalid mcp_servers in %s", path)
		}
	}
	quotedBinary, err := json.Marshal(binary)
	if err != nil {
		return nil, false, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, []byte(fmt.Sprintf("\n[mcp_servers.agentboard]\ncommand = %s\nargs = [\"serve\"]\n", quotedBinary))...)
	return data, true, nil
}
