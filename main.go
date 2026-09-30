package main

import (
	_ "embed"
	"os"

	"github.com/yildizozan/agentboard/cmd"
)

//go:embed skill/agentboard/SKILL.md
var agentboardSkill []byte

// version defaults to the source release and is overridden at release time with -ldflags "-X main.version=...".
var version = "0.2.0"

func main() {
	os.Exit(cmd.Execute(version, agentboardSkill))
}
