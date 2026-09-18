package command

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/telgatech/gpp/compiler"
)

type docOptions struct {
	all      bool
	json     bool
	location bool
	source   bool
	verbose  bool
	search   string
	lang     string
}

func runDoc(args []string) int {
	if wantsHelp(args) {
		return commandHelp("doc")
	}
	flags := flag.NewFlagSet("gpp doc", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := docOptions{}
	flags.BoolVar(&options.all, "all", false, "include private declarations")
	flags.BoolVar(&options.json, "json", false, "emit JSON documentation")
	flags.BoolVar(&options.location, "location", false, "show source locations")
	flags.BoolVar(&options.source, "source", false, "show source-level declarations")
	flags.BoolVar(&options.verbose, "verbose", false, "include additional metadata")
	flags.BoolVar(&options.verbose, "v", false, "include additional metadata")
	flags.StringVar(&options.search, "search", "", "search symbol names")
	flags.StringVar(&options.search, "s", "", "search symbol names")
	flags.StringVar(&options.lang, "lang", "", "show a language documentation topic")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(flags.Args()) > 1 {
		fmt.Fprintln(os.Stderr, "gpp doc accepts at most one symbol or package query")
		return 2
	}
	query := ""
	if len(flags.Args()) == 1 {
		query = flags.Args()[0]
	}
	if options.lang != "" {
		query = options.lang
	}

	if options.search != "" {
		return renderDocSearch(options.search, options)
	}
	if query == "" {
		return renderCurrentPackageDoc(options)
	}
	if topic, ok := languageDocTopic(query); ok {
		fmt.Println(topic)
		return 0
	}

	files, normalizedQuery, packageName, err := documentationFiles(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	index := compiler.BuildDocIndex(files)
	if packageName != "" && normalizedQuery == packageName {
		return printDocPackage(index, packageName, options)
	}
	results := index.Find(normalizedQuery, options.all)
	if len(results) == 0 {
		if nativeDoc(normalizedQuery) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "symbol not found: %s\n", query)
		return 1
	}
	if len(results) > 1 && !options.json {
		fmt.Fprintf(os.Stderr, "symbol %q is ambiguous\n\nCandidates:\n", query)
		for _, result := range results {
			fmt.Printf("    %s\n", displayDocName(*result))
		}
		return 1
	}
	if options.json {
		return printDocJSON(results)
	}
	for index, result := range results {
		if index > 0 {
			fmt.Println()
		}
		fmt.Print(renderDocSymbol(*result, options))
	}
	return 0
}

func renderCurrentPackageDoc(options docOptions) int {
	sources, err := discoverSources([]string{"."})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	files, err := parseDocumentationFiles(sources)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	index := compiler.BuildDocIndex(files)
	packageName := "main"
	if index.Packages[packageName] == nil {
		keys := make([]string, 0, len(index.Packages))
		for key := range index.Packages {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			fmt.Fprintln(os.Stderr, "no Go++ package documentation found")
			return 1
		}
		packageName = keys[0]
	}
	return printDocPackage(index, packageName, options)
}

func printDocPackage(index *compiler.DocIndex, packageName string, options docOptions) int {
	pkg := index.Packages[packageName]
	if pkg == nil {
		fmt.Fprintf(os.Stderr, "package not found: %s\n", packageName)
		return 1
	}
	if options.json {
		copy := *pkg
		copy.Symbols = make([]compiler.DocSymbol, 0, len(pkg.Symbols))
		for _, symbol := range pkg.Symbols {
			if options.all || symbol.Exported {
				copy.Symbols = append(copy.Symbols, symbol)
			}
		}
		return printJSON(copy)
	}
	fmt.Println(compiler.FormatDocPackage(index, packageName, options.all))
	return 0
}

func renderDocSearch(term string, options docOptions) int {
	sources, err := discoverSources([]string{"."})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	files, err := parseDocumentationFiles(sources)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if prelude, preludeErr := compiler.LoadPrelude(); preludeErr == nil {
		files = append(files, prelude)
	}
	results := compiler.BuildDocIndex(files).Search(term, options.all)
	if options.json {
		return printDocJSON(results)
	}
	for _, result := range results {
		fmt.Println(displayDocName(*result))
	}
	return 0
}

func documentationFiles(query string) ([]*compiler.File, string, string, error) {
	for _, packagePath := range compiler.OfficialStdlibPackages() {
		if query != packagePath && !strings.HasPrefix(query, packagePath+".") {
			continue
		}
		files, err := compiler.LoadOfficialPackage(packagePath)
		if err != nil {
			return nil, "", "", err
		}
		packageName := strings.ReplaceAll(packagePath, "/", ".")
		if query == packagePath {
			return files, packageName, packageName, nil
		}
		return files, packageName + strings.TrimPrefix(query, packagePath), packageName, nil
	}
	sources, err := discoverSources([]string{"."})
	if err != nil {
		return nil, "", "", err
	}
	files, err := parseDocumentationFiles(sources)
	if err != nil {
		return nil, "", "", err
	}
	if prelude, preludeErr := compiler.LoadPrelude(); preludeErr == nil {
		files = append(files, prelude)
	}
	return files, query, "", nil
}

func parseDocumentationFiles(sources []string) ([]*compiler.File, error) {
	files := make([]*compiler.File, 0, len(sources))
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		file, err := compiler.ParseFile(source, string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		file.SourcePath, err = filepath.Abs(source)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func printDocJSON(values []*compiler.DocSymbol) int {
	var output any = values
	if len(values) == 1 {
		output = values[0]
	}
	return printJSON(output)
}

func printJSON(output any) int {
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(string(data))
	return 0
}

func renderDocSymbol(symbol compiler.DocSymbol, options docOptions) string {
	var out strings.Builder
	if options.source && symbol.Source != "" {
		out.WriteString(symbol.Source)
		out.WriteString("\n")
	} else {
		out.WriteString(symbolHeader(symbol))
		out.WriteString("\n")
	}
	if symbol.Doc != "" {
		out.WriteString("\n")
		out.WriteString(symbol.Doc)
		out.WriteString("\n")
	}
	if options.location && symbol.SourceFile != "" {
		fmt.Fprintf(&out, "\nLocation: %s", symbol.SourceFile)
		if symbol.SourceLine > 0 {
			fmt.Fprintf(&out, ":%d", symbol.SourceLine)
		}
		out.WriteString("\n")
	}
	if len(symbol.Parents) > 0 {
		out.WriteString("\nParents:\n")
		for _, parent := range symbol.Parents {
			fmt.Fprintf(&out, "    %s\n", parent)
		}
	}
	if len(symbol.Fields) > 0 {
		out.WriteString("\nFields:\n")
		for _, field := range symbol.Fields {
			fmt.Fprintf(&out, "    %s %s\n", field.Name, field.Type)
			if options.verbose && field.Doc != "" {
				fmt.Fprintf(&out, "        %s\n", field.Doc)
			}
		}
	}
	if len(symbol.Methods) > 0 {
		out.WriteString("\nMethods:\n")
		for _, method := range symbol.Methods {
			fmt.Fprintf(&out, "    %s\n", method.Signature)
		}
	}
	if len(symbol.Static) > 0 {
		out.WriteString("\nStatic Methods:\n")
		for _, method := range symbol.Static {
			fmt.Fprintf(&out, "    %s\n", method.Signature)
		}
	}
	if len(symbol.Values) > 0 {
		out.WriteString("\nValues:\n")
		for _, value := range symbol.Values {
			fmt.Fprintf(&out, "    %s = %s\n", value.Name, value.Value)
		}
	}
	if len(symbol.Annotations) > 0 {
		out.WriteString("\nAnnotations:\n")
		for _, annotation := range symbol.Annotations {
			fmt.Fprintf(&out, "    %s\n", annotation)
		}
	}
	if options.verbose && symbol.Generated {
		out.WriteString("\nGenerated from Go++ source metadata.\n")
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

func symbolHeader(symbol compiler.DocSymbol) string {
	if symbol.Signature != "" {
		return symbol.Signature
	}
	switch symbol.Kind {
	case compiler.DocClass:
		return "class " + symbol.Name
	case compiler.DocEnum:
		return "enum " + symbol.Name
	case compiler.DocField:
		return "field " + symbol.FullName + " " + symbol.Signature
	case compiler.DocValue:
		return symbol.Signature
	default:
		return string(symbol.Kind) + " " + displayDocName(symbol)
	}
}

func displayDocName(symbol compiler.DocSymbol) string {
	if symbol.Kind == compiler.DocExtension {
		return symbol.Target + "." + symbol.Name
	}
	return strings.ReplaceAll(symbol.FullName, "gpp.", "gpp/")
}

func nativeDoc(query string) bool {
	command := exec.Command("go", "doc", query)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run() == nil
}

func languageDocTopic(query string) (string, bool) {
	topics := map[string]string{
		"try":        "try / catch / finally\n\nGo++ handles thrown errors with try, catch, and finally blocks.",
		"catch":      "try / catch / finally\n\ncatch handles Go++ thrown errors.",
		"class":      "class\n\nA class defines fields, methods, constructors, and optional parent classes.",
		"extend":     "extend\n\nAn extend block adds methods to an existing type without changing its declaration.",
		"record":     "record\n\nA record expression creates an anonymous structural value.",
		"template":   "template\n\nA template declaration defines a typed html/template source.",
		"annotation": "annotation\n\nAn annotation declaration defines typed metadata usable on supported source declarations.",
		"??":         "operator ??\n\nExpression-level error fallback evaluates the right side only when the left side fails.",
	}
	value, ok := topics[query]
	return value, ok
}
