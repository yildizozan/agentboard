package main

import (
	"os"

	"github.com/yildizozan/agentboard/internal/cli"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Execute(version))
}
