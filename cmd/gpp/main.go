package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gpp/compiler"
)

func main() {
	command := "compile"
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "build" || args[0] == "run") {
		command = args[0]
		args = args[1:]
	}

	flags := flag.NewFlagSet("gpp "+command, flag.ExitOnError)
	modulePath := flags.String(
		"module",
		"generated",
		"module path for generated Go code",
	)
	outputDir := flags.String(
		"output",
		".gpp",
		"directory for generated Go code",
	)
	noPrelude := flags.Bool(
		"no-prelude",
		false,
		"disable the implicit Go++ prelude",
	)
	noStdlib := flags.Bool(
		"no-stdlib",
		false,
		"disable the bundled official gpp standard module",
	)
	binaryPath := flags.String(
		"o",
		"",
		"output executable path for the build command",
	)
	if err := flags.Parse(args); err != nil {
		os.Exit(2)
	}

	if flags.NArg() == 0 {
		fmt.Fprintln(
			os.Stderr,
			"usage: gpp [build|run] [-module module/path] [-output directory] [-no-prelude] [-no-stdlib] file.gpp [file.gpp ...]",
		)

		os.Exit(1)
	}

	err := compiler.CompileFilesWithOptions(
		flags.Args(),
		*outputDir,
		compiler.CompileOptions{
			ModulePath:  *modulePath,
			NoPrelude:   *noPrelude,
			NoStdlib:    *noStdlib,
			CleanOutput: command == "build" || command == "run",
		},
	)

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if command == "compile" {
		return
	}

	if err := runGoCommand(*outputDir, "mod", "tidy"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if command == "build" {
		buildArgs := []string{"build"}
		if *binaryPath != "" {
			absolute, err := filepath.Abs(*binaryPath)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			buildArgs = append(buildArgs, "-o", absolute)
		}
		buildArgs = append(buildArgs, ".")
		if err := runGoCommand(*outputDir, buildArgs...); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := runGoCommand(*outputDir, "run", "."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runGoCommand(directory string, args ...string) error {
	command := exec.Command("go", args...)
	command.Dir = directory
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("go %s failed: %w", args[0], err)
	}
	return nil
}
