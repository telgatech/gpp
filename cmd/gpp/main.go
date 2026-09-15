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
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintln(
			os.Stderr,
			"usage: gpp [-module module/path] [-output directory] file.gpp [file.gpp ...]",
		)

		os.Exit(1)
	}

	err := compiler.CompileFilesWithOptions(
		flag.Args(),
		*outputDir,
		compiler.CompileOptions{ModulePath: *modulePath},
	)

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
