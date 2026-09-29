package main

import (
	"os"

	"github.com/yildizozan/agentboard/cmd"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cmd.Execute(version))
}
