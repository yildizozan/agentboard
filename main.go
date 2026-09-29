package main

import (
	_ "embed"
	"os"

	"github.com/yildizozan/agentboard/cmd"
)

//go:embed skill/agentboard/SKILL.md
var agentboardSkill []byte

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cmd.Execute(version, agentboardSkill))
}
