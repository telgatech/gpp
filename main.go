package main

import (
	"os"

	"github.com/telgatech/gpp/internal/command"
)

func main() {
	os.Exit(command.Run(os.Args[1:]))
}
