package main

import (
	"os"

	"github.com/yildizozan/agentboard/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
