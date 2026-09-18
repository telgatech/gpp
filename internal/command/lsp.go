package command

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/telgatech/gpp/internal/lsp"
)

func runLSP(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		return commandHelp("lsp")
	}
	logWriter := io.Writer(os.Stderr)
	var logFile *os.File
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--log" {
			if index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
				index++
				if err := setLSPLog(args[index], &logWriter, &logFile); err != nil {
					return reportError(err)
				}
				continue
			} else {
				continue
			}
		}
		if strings.HasPrefix(arg, "--log=") {
			if err := setLSPLog(strings.TrimPrefix(arg, "--log="), &logWriter, &logFile); err != nil {
				return reportError(err)
			}
			continue
		}
		return reportError(fmt.Errorf("unknown lsp option %q", arg))
	}
	if logFile != nil {
		defer logFile.Close()
	}
	if err := lsp.Run(os.Stdin, os.Stdout, logWriter); err != nil {
		return reportError(err)
	}
	return 0
}

func setLSPLog(path string, writer *io.Writer, file **os.File) error {
	if path == "" {
		return errors.New("--log requires a path when using --log=path")
	}
	opened, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open LSP log: %w", err)
	}
	*file = opened
	*writer = opened
	return nil
}
