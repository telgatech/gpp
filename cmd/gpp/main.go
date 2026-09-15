package main

import (
	"flag"
	"fmt"
	"os"

	"gpp/compiler"
)

func main() {
	modulePath := flag.String(
		"module",
		"generated",
		"module path for generated Go code",
	)
	outputDir := flag.String(
		"output",
		".gpp",
		"directory for generated Go code",
	)
	noPrelude := flag.Bool(
		"no-prelude",
		false,
		"disable the implicit Go++ prelude",
	)
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintln(
			os.Stderr,
			"usage: gpp [-module module/path] [-output directory] [-no-prelude] file.gpp [file.gpp ...]",
		)

		os.Exit(1)
	}

	err := compiler.CompileFilesWithOptions(
		flag.Args(),
		*outputDir,
		compiler.CompileOptions{ModulePath: *modulePath, NoPrelude: *noPrelude},
	)

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
