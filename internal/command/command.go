package command

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/telgatech/gpp/compiler"
)

// Version is overridden by release builds when desired. Keeping a stable
// development value makes `gpp version` useful from source checkouts too.
var Version = "0.1.0"

const minimumGoMinor = 26

type compileFlags struct {
	module      string
	output      string
	development bool
	noPrelude   bool
	noStdlib    bool
	emitGo      bool
	binaryPath  string
	test        testSelection
}

type testSelection struct {
	suites   stringList
	tags     stringList
	tagsAll  string
	priority string
	run      string
	list     bool
	failFast bool
	cover    bool
	race     bool
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func Run(args []string) int {
	if len(args) == 0 {
		printHelp()
		return 0
	}

	commandName := args[0]
	commandArgs := args[1:]
	switch commandName {
	case "init":
		return runInit(commandArgs)
	case "build":
		return runBuild(commandArgs)
	case "run":
		return runRun(commandArgs)
	case "clean":
		return runClean(commandArgs)
	case "fmt":
		return runFmt(commandArgs)
	case "test":
		return runTest(commandArgs)
	case "doc":
		return runDoc(commandArgs)
	case "env":
		return runEnv(commandArgs)
	case "doctor":
		return runDoctor(commandArgs)
	case "version":
		return runVersion(commandArgs)
	case "lsp":
		return runLSP(commandArgs)
	case "help", "-h", "--help":
		if len(commandArgs) > 0 {
			return commandHelp(commandArgs[0])
		}
		printHelp()
		return 0
	}

	// Preserve the original transpiler invocation while the subcommand CLI is
	// adopted: `gpp file.gpp` and `gpp -module example.com/app file.gpp`.
	if commandName == "compile" || isGoPlusSource(commandName) || strings.HasPrefix(commandName, "-") {
		return runCompile(args)
	}

	fatalf("unknown command %q; run `gpp --help` for usage", commandName)
	return 2
}

func printHelp() {
	fmt.Println("Go++ compiler and toolchain")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("    gpp <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("    init       create a Go++ project")
	fmt.Println("    build      build a Go++ program")
	fmt.Println("    run        build and run a Go++ program")
	fmt.Println("    clean      remove generated build artifacts")
	fmt.Println("    fmt        format Go++ source")
	fmt.Println("    test       run Go++ tests")
	fmt.Println("    doc        show Go++ source documentation")
	fmt.Println("    env        show environment information")
	fmt.Println("    doctor     diagnose the toolchain")
	fmt.Println("    version    show version information")
	fmt.Println("    lsp        start the Language Server Protocol service")
}

func commandHelp(name string) int {
	switch name {
	case "compile":
		fmt.Println("Usage: gpp [options] <file.gpp|file.gpp.tpl|directory>")
		printCompileFlags()
	case "init":
		fmt.Println("Usage: gpp init [directory]")
	case "build":
		fmt.Println("Usage: gpp build [options] <file.gpp|file.gpp.tpl|directory>")
		fmt.Println("  -o path          output executable path")
		fmt.Println("  -emit-go         retain and report generated Go source")
		printCompileFlags()
	case "run":
		fmt.Println("Usage: gpp run [options] <file.gpp|file.gpp.tpl|directory> [-- program arguments]")
		printCompileFlags()
	case "clean":
		fmt.Println("Usage: gpp clean [-output directory]")
	case "fmt":
		fmt.Println("Usage: gpp fmt [file.gpp|file.gpp.tpl|directory ...]")
	case "test":
		fmt.Println("Usage: gpp test [options] [directory|./...]")
		printCompileFlags()
	case "doc":
		fmt.Println("Usage: gpp doc [options] [symbol]")
		fmt.Println("  --all            include private declarations")
		fmt.Println("  --json           emit machine-readable documentation")
		fmt.Println("  --location       show source locations")
		fmt.Println("  --source         show source-level declaration signatures")
		fmt.Println("  --search, -s     search symbol names")
		fmt.Println("  --verbose, -v    include additional metadata")
	case "env":
		fmt.Println("Usage: gpp env")
	case "doctor":
		fmt.Println("Usage: gpp doctor")
	case "version":
		fmt.Println("Usage: gpp version")
	case "lsp":
		fmt.Println("Usage: gpp lsp [--log[=path]]")
	default:
		fatalf("unknown command %q; run `gpp --help` for usage", name)
		return 2
	}
	return 0
}

func printCompileFlags() {
	fmt.Println("  -module path      module path for generated Go code")
	fmt.Println("  -output directory directory for generated Go code")
	fmt.Println("  -no-prelude       disable the implicit Go++ prelude")
	fmt.Println("  -no-stdlib        disable bundled official Go++ packages")
}

func parseCompileFlags(name string, args []string, allowOutputBinary bool) (compileFlags, []string, []string, int) {
	args = reorderCompileArgs(args, allowOutputBinary, name == "test")
	flags := flag.NewFlagSet("gpp "+name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	result := compileFlags{}
	flags.StringVar(&result.module, "module", "generated", "module path for generated Go code")
	flags.StringVar(&result.output, "output", ".gpp", "directory for generated Go code")
	flags.BoolVar(&result.noPrelude, "no-prelude", false, "disable the implicit Go++ prelude")
	flags.BoolVar(&result.noStdlib, "no-stdlib", false, "disable bundled official Go++ packages")
	if allowOutputBinary {
		flags.StringVar(&result.binaryPath, "o", "", "output executable path")
		flags.BoolVar(&result.emitGo, "emit-go", false, "retain and report generated Go source")
	}
	if name == "test" {
		flags.Var(&result.test.suites, "suite", "select a Go++ test suite (repeatable)")
		flags.Var(&result.test.tags, "tag", "select a Go++ test tag (repeatable)")
		flags.StringVar(&result.test.tagsAll, "tags-all", "", "require all comma-separated Go++ tags")
		flags.StringVar(&result.test.priority, "priority", "", "select Go++ priorities: high,medium,low")
		flags.StringVar(&result.test.run, "run", "", "select test cases by name")
		flags.BoolVar(&result.test.list, "list", false, "list discovered tests without running them")
		flags.BoolVar(&result.test.failFast, "fail-fast", false, "stop after the first failed test phase")
		flags.BoolVar(&result.test.cover, "cover", false, "enable Go coverage")
		flags.BoolVar(&result.test.race, "race", false, "enable the Go race detector")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return result, nil, nil, 0
		}
		return result, nil, []string{err.Error()}, 2
	}
	return result, flags.Args(), nil, -1
}

// Go's flag package stops parsing at the first positional argument. The CLI
// accepts the natural `gpp build . -o app` form as well as flags-first, so
// move known compile flags ahead of source paths before parsing them.
func reorderCompileArgs(args []string, allowOutputBinary, testing bool) []string {
	var flags, positional []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			positional = append(positional, args[index:]...)
			break
		}
		name := strings.TrimPrefix(arg, "--")
		name = strings.TrimPrefix(name, "-")
		known := name == "module" || name == "output" || name == "no-prelude" || name == "no-stdlib" || (allowOutputBinary && (name == "o" || name == "emit-go"))
		if testing {
			known = known || name == "suite" || name == "tag" || name == "tags-all" || name == "priority" || name == "run" || name == "list" || name == "fail-fast" || name == "cover" || name == "race"
		}
		if !known {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		if name == "module" || name == "output" || name == "suite" || name == "tag" || name == "tags-all" || name == "priority" || name == "run" || (allowOutputBinary && name == "o") {
			if index+1 < len(args) && !strings.Contains(arg, "=") {
				index++
				flags = append(flags, args[index])
			}
		}
	}
	return append(flags, positional...)
}

func runCompile(args []string) int {
	if wantsHelp(args) {
		return commandHelp("compile")
	}
	options, positional, parseErrors, code := parseCompileFlags("compile", args, false)
	if code >= 0 {
		if code != 0 {
			for _, message := range parseErrors {
				fmt.Fprintln(os.Stderr, message)
			}
		}
		return code
	}
	if len(positional) == 0 {
		fatalf("no Go++ source files supplied; run `gpp --help` for usage")
		return 1
	}
	sources, err := discoverSources(positional)
	if err != nil {
		return reportError(err)
	}
	if err := compileSources(sources, options, false, false); err != nil {
		return reportError(err)
	}
	return 0
}

func runBuild(args []string) int {
	if wantsHelp(args) {
		return commandHelp("build")
	}
	options, positional, parseErrors, code := parseCompileFlags("build", args, true)
	if code >= 0 {
		if code != 0 {
			for _, message := range parseErrors {
				fmt.Fprintln(os.Stderr, message)
			}
		}
		return code
	}
	if len(positional) == 0 {
		positional = []string{"."}
	}
	if err := requireSupportedGo(); err != nil {
		return reportError(err)
	}
	sources, err := discoverSources(positional)
	if err != nil {
		return reportError(err)
	}
	if err := compileSources(sources, options, true, false); err != nil {
		return reportError(err)
	}
	if options.emitGo {
		fmt.Printf("generated Go: %s\n", options.output)
	}

	binaryPath := options.binaryPath
	if binaryPath == "" {
		binaryPath = defaultBinaryName(sources)
		if binaryPath == "." || binaryPath == string(filepath.Separator) || binaryPath == "" {
			binaryPath = "gpp-app"
		}
	}
	absoluteBinary, err := filepath.Abs(binaryPath)
	if err != nil {
		return reportError(err)
	}
	if err := os.MkdirAll(filepath.Dir(absoluteBinary), 0755); err != nil {
		return reportError(err)
	}
	if err := runGoCommand(options.output, "mod", "tidy"); err != nil {
		return reportError(err)
	}
	if err := runGoCommand(options.output, "build", "-o", absoluteBinary, "."); err != nil {
		return reportError(err)
	}
	fmt.Printf("built %s\n", binaryPath)
	return 0
}

func runRun(args []string) int {
	if wantsHelp(args) {
		return commandHelp("run")
	}
	separator := len(args)
	for index, arg := range args {
		if arg == "--" {
			separator = index
			break
		}
	}
	options, positional, parseErrors, code := parseCompileFlags("run", args[:separator], false)
	if code >= 0 {
		if code != 0 {
			for _, message := range parseErrors {
				fmt.Fprintln(os.Stderr, message)
			}
		}
		return code
	}
	options.development = true
	if len(positional) == 0 {
		positional = []string{"."}
	}
	if err := requireSupportedGo(); err != nil {
		return reportError(err)
	}
	sources, err := discoverSources(positional)
	if err != nil {
		return reportError(err)
	}
	if err := compileSources(sources, options, true, false); err != nil {
		return reportError(err)
	}
	if err := runGoCommand(options.output, "mod", "tidy"); err != nil {
		return reportError(err)
	}
	goArgs := []string{"run", "."}
	if separator < len(args) {
		goArgs = append(goArgs, args[separator+1:]...)
	}
	if err := runGoCommand(options.output, goArgs...); err != nil {
		return reportError(err)
	}
	return 0
}

func runTest(args []string) int {
	if wantsHelp(args) {
		return commandHelp("test")
	}
	options, positional, parseErrors, code := parseCompileFlags("test", args, false)
	if code >= 0 {
		if code != 0 {
			for _, message := range parseErrors {
				fmt.Fprintln(os.Stderr, message)
			}
		}
		return code
	}
	pattern := "."
	sourceArgs := positional
	nativeRoot := ""
	if len(positional) > 0 && !isSourcePath(positional[0]) {
		pattern = positional[0]
		sourceArgs = nil
	}
	if len(sourceArgs) == 0 {
		if root := moduleRoot(); root != "" {
			sourceArgs = []string{root}
			pattern = "./..."
			nativeRoot = root
		} else {
			sourceArgs = []string{"."}
		}
	}
	if err := requireSupportedGo(); err != nil {
		return reportError(err)
	}
	sources, err := discoverSources(sourceArgs)
	if err != nil {
		return reportError(err)
	}
	selection, err := discoverTestSelection(sources, options.test)
	if err != nil {
		return reportError(err)
	}
	if options.test.list {
		if err := compileSources(sources, options, true, true, selection); err != nil {
			return reportError(err)
		}
		if nativeRoot != "" {
			if err := copyNativeGoTree(nativeRoot, options.output); err != nil {
				return reportError(err)
			}
		}
		if err := runGoCommand(options.output, "mod", "tidy"); err != nil {
			return reportError(err)
		}
		printTestList(selection)
		if !selection.hasMetadataFilter() {
			nativeTests, err := listNativeGoTests(options.output, pattern, options.test.run)
			if err != nil {
				return reportError(err)
			}
			if len(nativeTests) > 0 {
				if len(selection.Cases) > 0 {
					fmt.Println("Go tests")
				}
				for _, name := range nativeTests {
					fmt.Println("    " + name)
				}
			}
		}
		return 0
	}
	if err := compileSources(sources, options, true, true, selection); err != nil {
		return reportError(err)
	}
	if nativeRoot != "" {
		if err := copyNativeGoTree(nativeRoot, options.output); err != nil {
			return reportError(err)
		}
	}
	if err := runGoCommand(options.output, "mod", "tidy"); err != nil {
		return reportError(err)
	}
	goArgs := []string{"test"}
	if options.test.cover {
		goArgs = append(goArgs, "-cover")
	}
	if options.test.race {
		goArgs = append(goArgs, "-race")
	}
	if options.test.failFast {
		goArgs = append(goArgs, "-failfast")
	}
	if selection.hasMetadataFilter() {
		goArgs = append(goArgs, "-run", "^TestGoPP_")
	} else if options.test.run != "" {
		goArgs = append(goArgs, "-run", goRunPattern(options.test.run))
	}
	goArgs = append(goArgs, pattern)
	if err := runGoCommand(options.output, goArgs...); err != nil {
		return reportError(err)
	}
	return 0
}

func compileSources(sources []string, options compileFlags, clean bool, includeTests bool, selections ...testSelectionResult) error {
	if err := compiler.CompileFilesWithOptions(sources, options.output, compiler.CompileOptions{
		ModulePath:  options.module,
		Development: options.development,
		NoPrelude:   options.noPrelude,
		NoStdlib:    options.noStdlib,
		CleanOutput: clean,
	}); err != nil {
		return err
	}
	if err := copyNativeGoFiles(sources, options.output); err != nil {
		return err
	}
	if !includeTests {
		return nil
	}
	if len(selections) != 1 {
		return errors.New("internal error: missing Go++ test selection")
	}
	return emitTestWrappers(selections[0], options.output)
}

// copyNativeGoFiles keeps ordinary Go helpers usable in a mixed project. The
// compiler owns the generated workspace, so native files are copied there
// only from directories that also contain a Go++ source file.
func copyNativeGoFiles(sources []string, output string) error {
	directories := map[string]bool{}
	for _, source := range sources {
		directories[filepath.Dir(source)] = true
	}
	for directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
				continue
			}
			source := filepath.Join(directory, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.PackageClauseOnly)
			if err != nil {
				return fmt.Errorf("parse native Go file %s: %w", source, err)
			}
			destinationDir := output
			if file.Name.Name != "main" {
				destinationDir = filepath.Join(output, file.Name.Name)
			}
			if err := os.MkdirAll(destinationDir, 0755); err != nil {
				return err
			}
			destination := filepath.Join(destinationDir, entry.Name())
			if _, err := os.Stat(destination); err == nil {
				return fmt.Errorf("native Go file %s conflicts with generated output %s", source, destination)
			} else if !os.IsNotExist(err) {
				return err
			}
			data, err := os.ReadFile(source)
			if err != nil {
				return err
			}
			if err := os.WriteFile(destination, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyNativeGoTree(root, output string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".gpp" || entry.Name() == ".git" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(output, relative)
		if filepath.Clean(path) == filepath.Clean(destination) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		if _, err := os.Stat(destination); err == nil {
			return fmt.Errorf("native Go file %s conflicts with generated output %s", path, destination)
		} else if !os.IsNotExist(err) {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0644)
	})
}

type testSelectionResult struct {
	Suites           []discoveredSuite
	Cases            []discoveredTestCase
	MetadataFiltered bool
}

type discoveredSuite struct {
	Package string
	Name    string
	Tags    []string
	Cases   []discoveredTestCase
}

type discoveredTestCase struct {
	Package  string
	Suite    string
	Method   string
	Priority string
	Tags     []string
}

func (selection testSelectionResult) hasMetadataFilter() bool {
	return selection.MetadataFiltered
}

func discoverTestSelection(sources []string, filters testSelection) (testSelectionResult, error) {
	classes := map[string]*compiler.ClassDecl{}
	packages := map[string]string{}
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return testSelectionResult{}, err
		}
		file, err := compiler.ParseFile(filepath.Base(source), string(data))
		if err != nil {
			return testSelectionResult{}, err
		}
		for _, declaration := range file.Decls {
			class, ok := declaration.(*compiler.ClassDecl)
			if !ok {
				continue
			}
			key := file.Package + "." + class.Name
			classes[key] = class
			packages[key] = file.Package
		}
	}

	var suites []discoveredSuite
	for key, class := range classes {
		packageName := packages[key]
		if !isSuiteClass(class, packageName, classes, map[string]bool{}) {
			continue
		}
		cases := effectiveTestCases(class, packageName, classes, map[string]bool{})
		if len(cases) == 0 {
			continue
		}
		suite := discoveredSuite{
			Package: packageName,
			Name:    class.Name,
			Tags:    classTags(class, packageName, classes, map[string]bool{}),
			Cases:   cases,
		}
		for index := range suite.Cases {
			suite.Cases[index].Package = packageName
			suite.Cases[index].Suite = class.Name
			if suite.Cases[index].Priority == "" {
				suite.Cases[index].Priority = classPriority(class, packageName, classes, map[string]bool{})
			}
			if suite.Cases[index].Priority == "" {
				suite.Cases[index].Priority = "medium"
			}
			suite.Cases[index].Tags = appendUniqueStrings(suite.Tags, suite.Cases[index].Tags...)
		}
		suites = append(suites, suite)
	}
	sort.Slice(suites, func(left, right int) bool {
		return suites[left].Package+"."+suites[left].Name < suites[right].Package+"."+suites[right].Name
	})

	validPriorities, err := selectedPriorities(filters.priority)
	if err != nil {
		return testSelectionResult{}, err
	}
	requestedSuite := map[string]bool{}
	for _, name := range filters.suites {
		requestedSuite[name] = true
	}
	result := testSelectionResult{MetadataFiltered: len(requestedSuite) > 0 || len(filters.tags) > 0 || filters.tagsAll != "" || filters.priority != ""}
	for _, suite := range suites {
		for _, testCase := range suite.Cases {
			qualified := suite.Name + "." + testCase.Method
			qualifiedPackage := suite.Package + "." + qualified
			if len(requestedSuite) > 0 && !requestedSuite[suite.Name] && !requestedSuite[qualified] && !requestedSuite[qualifiedPackage] {
				continue
			}
			if len(filters.tags) > 0 && !anyTag(testCase.Tags, filters.tags) {
				continue
			}
			if filters.tagsAll != "" && !allTags(testCase.Tags, splitCSV(filters.tagsAll)) {
				continue
			}
			if len(validPriorities) > 0 && !validPriorities[testCase.Priority] {
				continue
			}
			if filters.run != "" && !strings.Contains(strings.ToLower(qualified), strings.ToLower(filters.run)) {
				continue
			}
			result.Cases = append(result.Cases, testCase)
		}
	}
	if len(requestedSuite) > 0 {
		for requested := range requestedSuite {
			found := false
			for _, suite := range suites {
				if requested == suite.Name || requested == suite.Package+"."+suite.Name {
					found = true
					break
				}
			}
			if !found {
				return testSelectionResult{}, fmt.Errorf("suite not found: %s", requested)
			}
		}
	}
	if (len(filters.tags) > 0 || filters.tagsAll != "" || filters.priority != "" || filters.run != "") && len(result.Cases) == 0 {
		return testSelectionResult{}, errors.New("no Go++ test cases matched the requested filters")
	}
	result.Suites = suites
	return result, nil
}

func emitTestWrappers(selection testSelectionResult, output string) error {
	byPackage := map[string][]discoveredTestCase{}
	for _, testCase := range selection.Cases {
		byPackage[testCase.Package] = append(byPackage[testCase.Package], testCase)
	}
	for packageName, cases := range byPackage {
		directory := output
		for _, segment := range strings.Split(packageName, ".") {
			if segment != "main" {
				directory = filepath.Join(directory, segment)
			}
		}
		if err := os.MkdirAll(directory, 0755); err != nil {
			return err
		}
		sort.Slice(cases, func(left, right int) bool {
			if cases[left].Priority != cases[right].Priority {
				return priorityRank(cases[left].Priority) < priorityRank(cases[right].Priority)
			}
			return cases[left].Suite+"."+cases[left].Method < cases[right].Suite+"."+cases[right].Method
		})
		var source strings.Builder
		fmt.Fprintf(&source, "package %s\n\nimport \"testing\"\n\n", goPackageName(packageName))
		source.WriteString("func __gppRunSuiteCase(t *testing.T, setup, body, teardown func()) {\n")
		source.WriteString("\tdefer func() {\n")
		source.WriteString("\t\tprimary := recover()\n")
		source.WriteString("\t\tvar cleanup any\n")
		source.WriteString("\t\tfunc() { defer func() { cleanup = recover() }(); teardown() }()\n")
		source.WriteString("\t\tif primary != nil { if thrown, ok := primary.(interface{ GppThrownError() error }); ok { t.Errorf(\"Go++ test failed: %v\", thrown.GppThrownError()) } else { panic(primary) } }\n")
		source.WriteString("\t\tif cleanup != nil { if thrown, ok := cleanup.(interface{ GppThrownError() error }); ok { t.Errorf(\"Go++ teardown failed: %v\", thrown.GppThrownError()) } else { panic(cleanup) } }\n")
		source.WriteString("\t}()\n\tsetup()\n\tbody()\n}\n\n")
		for _, testCase := range cases {
			// The numeric prefix makes Go's test discovery preserve Go++'s
			// priority phases even though the underlying runner only sees Go
			// test functions. High, medium, and low therefore sort as 0, 1, 2.
			functionName := fmt.Sprintf("TestGoPP_%d_%s_%s_%s", priorityRank(testCase.Priority), testCase.Priority, testCase.Suite, testCase.Method)
			fmt.Fprintf(&source, "func %s(t *testing.T) {\n", functionName)
			fmt.Fprintf(&source, "\tsuite := &%s{}\n", testCase.Suite)
			source.WriteString("\tsuite.Attach(t)\n")
			source.WriteString("\t__gppRunSuiteCase(t, suite.Setup, suite.")
			source.WriteString(testCase.Method)
			source.WriteString(", suite.Teardown)\n}\n\n")
		}
		if err := os.WriteFile(filepath.Join(directory, "gpp_suites_test.go"), []byte(source.String()), 0644); err != nil {
			return err
		}
	}
	return nil
}

func priorityRank(priority string) int {
	switch priority {
	case "high":
		return 0
	case "low":
		return 2
	default:
		return 1
	}
}

func printTestList(selection testSelectionResult) {
	selected := map[string][]discoveredTestCase{}
	for _, testCase := range selection.Cases {
		key := testCase.Package + "." + testCase.Suite
		selected[key] = append(selected[key], testCase)
	}
	for _, suite := range selection.Suites {
		cases := selected[suite.Package+"."+suite.Name]
		if len(cases) == 0 {
			continue
		}
		if suite.Package == "main" {
			fmt.Println(suite.Name)
		} else {
			fmt.Printf("%s.%s\n", suite.Package, suite.Name)
		}
		for _, testCase := range cases {
			fmt.Printf("    %-20s %-7s %s\n", testCase.Method, testCase.Priority, strings.Join(testCase.Tags, ","))
		}
	}
	if len(selection.Cases) == 0 && len(selection.Suites) == 0 {
		fmt.Println("Go tests")
	}
}

func listNativeGoTests(directory, packagePattern, run string) ([]string, error) {
	goPath, err := findGo()
	if err != nil {
		return nil, err
	}
	listPattern := "."
	if run != "" {
		listPattern = goRunPattern(run)
	}
	command := exec.Command(goPath, "test", "-list", listPattern, packagePattern)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go test -list failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	seen := map[string]bool{}
	var tests []string
	for _, line := range strings.Split(string(output), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "TestGoPP_") {
			continue
		}
		if strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") || strings.HasPrefix(name, "Fuzz") {
			if !seen[name] {
				seen[name] = true
				tests = append(tests, name)
			}
		}
	}
	sort.Strings(tests)
	return tests, nil
}

func isSuiteClass(class *compiler.ClassDecl, packageName string, classes map[string]*compiler.ClassDecl, visiting map[string]bool) bool {
	key := packageName + "." + class.Name
	if visiting[key] {
		return false
	}
	visiting[key] = true
	defer delete(visiting, key)
	for _, parent := range class.Parents {
		if parent == "test.Suite" || strings.HasSuffix(parent, ".Suite") {
			return true
		}
		if base := classes[packageName+"."+parent]; base != nil && isSuiteClass(base, packageName, classes, visiting) {
			return true
		}
		for candidateKey, candidate := range classes {
			if strings.HasSuffix(candidateKey, "."+parent) && isSuiteClass(candidate, packagesForClass(candidateKey), classes, visiting) {
				return true
			}
		}
	}
	return false
}

func packagesForClass(key string) string {
	index := strings.LastIndex(key, ".")
	if index < 0 {
		return "main"
	}
	return key[:index]
}

func effectiveTestCases(class *compiler.ClassDecl, packageName string, classes map[string]*compiler.ClassDecl, visiting map[string]bool) []discoveredTestCase {
	key := packageName + "." + class.Name
	if visiting[key] {
		return nil
	}
	visiting[key] = true
	defer delete(visiting, key)
	methods := map[string]compiler.Method{}
	for _, parent := range class.Parents {
		if base := classes[packageName+"."+parent]; base != nil {
			for _, testCase := range effectiveMethods(base, packageName, classes, visiting) {
				methods[testCase.Method] = testCaseMethod(testCase)
			}
		}
	}
	for _, method := range class.Methods {
		methods[method.Name] = method
	}
	var result []discoveredTestCase
	for _, method := range methods {
		if method.IsStatic || method.Parameters != "" || method.Result != "" || !isPublicTestName(method.Name) || reservedTestMethod(method.Name) {
			continue
		}
		result = append(result, discoveredTestCase{Method: method.Name, Priority: annotationPriority(method.Annotations), Tags: annotationTags(method.Annotations)})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Method < result[right].Method })
	return result
}

func effectiveMethods(class *compiler.ClassDecl, packageName string, classes map[string]*compiler.ClassDecl, visiting map[string]bool) []discoveredTestCase {
	return effectiveTestCases(class, packageName, classes, visiting)
}

func testCaseMethod(testCase discoveredTestCase) compiler.Method {
	return compiler.Method{Name: testCase.Method, Annotations: nil}
}

func classTags(class *compiler.ClassDecl, packageName string, classes map[string]*compiler.ClassDecl, visiting map[string]bool) []string {
	key := packageName + "." + class.Name
	if visiting[key] {
		return nil
	}
	visiting[key] = true
	defer delete(visiting, key)
	result := annotationTags(class.Annotations)
	for _, parent := range class.Parents {
		if base := classes[packageName+"."+parent]; base != nil {
			result = appendUniqueStrings(result, classTags(base, packageName, classes, visiting)...)
		}
	}
	return result
}

func classPriority(class *compiler.ClassDecl, packageName string, classes map[string]*compiler.ClassDecl, visiting map[string]bool) string {
	if priority := annotationPriority(class.Annotations); priority != "" {
		return priority
	}
	for _, parent := range class.Parents {
		if base := classes[packageName+"."+parent]; base != nil {
			if priority := classPriority(base, packageName, classes, visiting); priority != "" {
				return priority
			}
		}
	}
	return ""
}

func annotationTags(annotations []compiler.AnnotationUse) []string {
	var result []string
	for _, annotation := range annotations {
		if annotation.Name != "Tag" && !strings.HasSuffix(annotation.Name, ".Tag") {
			continue
		}
		value := strings.TrimSpace(annotation.Arguments)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		if value != "" {
			result = appendUniqueStrings(result, strings.ToLower(value))
		}
	}
	return result
}

func annotationPriority(annotations []compiler.AnnotationUse) string {
	for _, annotation := range annotations {
		if annotation.Name != "Priority" && !strings.HasSuffix(annotation.Name, ".Priority") {
			continue
		}
		value := strings.TrimSpace(annotation.Arguments)
		if index := strings.LastIndex(value, "."); index >= 0 {
			value = value[index+1:]
		}
		return strings.ToLower(value)
	}
	return ""
}

func isPublicTestName(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func reservedTestMethod(name string) bool {
	switch name {
	case "Setup", "Teardown", "Attach", "Equal", "NotEqual", "True", "False", "Nil", "NotNil", "Same", "NotSame", "Includes", "NotIncludes", "Empty", "NotEmpty", "Match", "NotMatch", "InDelta", "Fail", "Skip", "Throws", "Log", "Logf", "TempDir", "Parallel", "Helper", "Cleanup", "Name":
		return true
	default:
		return false
	}
}

func selectedPriorities(value string) (map[string]bool, error) {
	selected := map[string]bool{}
	for _, priority := range splitCSV(value) {
		priority = strings.ToLower(priority)
		if priority != "high" && priority != "medium" && priority != "low" {
			return nil, fmt.Errorf("unknown test priority %q", priority)
		}
		selected[priority] = true
	}
	return selected, nil
}

func splitCSV(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func anyTag(actual []string, requested []string) bool {
	for _, want := range requested {
		for _, have := range actual {
			if strings.EqualFold(want, have) {
				return true
			}
		}
	}
	return false
}

func allTags(actual []string, requested []string) bool {
	return len(requested) > 0 && func() bool {
		for _, want := range requested {
			found := false
			for _, have := range actual {
				if strings.EqualFold(want, have) {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	}()
}

func goRunPattern(pattern string) string {
	return strings.ReplaceAll(pattern, ".", "[_\\.]")
}

func moduleRoot() string {
	working, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(working, "go.mod")); err == nil {
			return working
		}
		parent := filepath.Dir(working)
		if parent == working {
			return ""
		}
		working = parent
	}
}

func goPackageName(packageName string) string {
	parts := strings.Split(packageName, ".")
	return parts[len(parts)-1]
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		found := false
		for _, value := range values {
			if value == addition {
				found = true
				break
			}
		}
		if !found {
			values = append(values, addition)
		}
	}
	return values
}

func discoverSources(args []string) ([]string, error) {
	var sources []string
	seen := map[string]bool{}
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("source path %q: %w", arg, err)
		}
		if !info.IsDir() {
			if !isGoPlusSource(arg) {
				return nil, fmt.Errorf("source file %q is not a .gpp or .gpp.tpl file", arg)
			}
			absolute, err := filepath.Abs(arg)
			if err != nil {
				return nil, err
			}
			if !seen[absolute] {
				sources = append(sources, absolute)
				seen[absolute] = true
			}
			continue
		}
		err = filepath.WalkDir(arg, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != arg && (entry.Name() == ".gpp" || entry.Name() == ".git" || entry.Name() == "vendor") {
					return filepath.SkipDir
				}
				return nil
			}
			if !isGoPlusSource(path) {
				return nil
			}
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if !seen[absolute] {
				sources = append(sources, absolute)
				seen[absolute] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(sources)
	if len(sources) == 0 {
		return nil, fmt.Errorf("no .gpp or .gpp.tpl source files found")
	}
	return sources, nil
}

func isSourcePath(path string) bool {
	if isGoPlusSource(path) {
		return true
	}
	_, err := os.Stat(path)
	return err == nil
}

func isGoPlusSource(path string) bool {
	return strings.HasSuffix(path, ".gpp") || strings.HasSuffix(path, ".gpp.tpl")
}

func runGoCommand(directory string, args ...string) error {
	goPath, err := findGo()
	if err != nil {
		return err
	}
	command := exec.Command(goPath, args...)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if stdout.Len() > 0 {
			_, _ = os.Stdout.Write(stdout.Bytes())
		}
		if output := cleanBackendOutput(stderr.String(), directory); output != "" {
			return backendDiagnosticError{output: output}
		}
		return fmt.Errorf("Go backend failed: %w", err)
	}
	if stdout.Len() > 0 {
		_, _ = os.Stdout.Write(stdout.Bytes())
	}
	if stderr.Len() > 0 {
		_, _ = os.Stderr.Write(stderr.Bytes())
	}
	return nil
}

type backendDiagnosticError struct {
	output string
}

func (err backendDiagnosticError) Error() string {
	return err.output
}

func cleanBackendOutput(output, directory string) string {
	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "# generated") {
			continue
		}
		line = normalizeBackendLocation(line, directory)
		filtered = append(filtered, line)
	}
	output = strings.TrimRight(strings.Join(filtered, "\n"), "\n")
	return output
}

func normalizeBackendLocation(line, directory string) string {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 || colon+1 >= len(line) || line[colon+1] < '0' || line[colon+1] > '9' {
		return line
	}
	location := line[:colon]
	working, err := os.Getwd()
	if err != nil {
		return line
	}
	base, err := filepath.Abs(directory)
	if err != nil {
		return line
	}
	absolute := location
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(base, filepath.FromSlash(absolute))
	}
	relative, err := filepath.Rel(working, absolute)
	if err != nil {
		return line
	}
	return filepath.ToSlash(relative) + line[colon:]
}

func findGo() (string, error) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return "", errors.New("Go compiler not found; install Go and ensure `go` is available on PATH")
	}
	return goPath, nil
}

func goVersion() (string, error) {
	goPath, err := findGo()
	if err != nil {
		return "", err
	}
	output, err := exec.Command(goPath, "version").Output()
	if err != nil {
		return "", fmt.Errorf("could not determine Go version: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) < 3 {
		return "", fmt.Errorf("unexpected Go version output %q", strings.TrimSpace(string(output)))
	}
	return fields[2], nil
}

func supportedGo(version string) bool {
	version = strings.TrimPrefix(version, "go")
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return false
	}
	return major > 1 || (major == 1 && minor >= minimumGoMinor)
}

func requireSupportedGo() error {
	version, err := goVersion()
	if err != nil {
		return err
	}
	if !supportedGo(version) {
		return fmt.Errorf("Go %s is unsupported; Go 1.%d or newer is required", version, minimumGoMinor)
	}
	return nil
}

func defaultBinaryName(sources []string) string {
	if len(sources) == 1 {
		name := filepath.Base(sources[0])
		if strings.HasSuffix(name, ".gpp.tpl") {
			name = strings.TrimSuffix(name, ".gpp.tpl")
		} else {
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		if name != "" && name != "." {
			return name
		}
	}
	return filepath.Base(mustGetwd())
}

func runInit(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		return commandHelp("init")
	}
	if len(args) > 1 {
		fatalf("init accepts at most one directory")
		return 2
	}
	target := "."
	if len(args) == 1 {
		target = args[0]
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return reportError(err)
	}
	mainPath := filepath.Join(target, "main.gpp")
	if _, err := os.Stat(mainPath); err == nil {
		return reportError(fmt.Errorf("refusing to overwrite existing %s", mainPath))
	} else if !os.IsNotExist(err) {
		return reportError(err)
	}
	const starter = "import \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello from Go++\")\n}\n"
	if err := os.WriteFile(mainPath, []byte(starter), 0644); err != nil {
		return reportError(err)
	}
	fmt.Printf("created %s\n", mainPath)
	return 0
}

func runClean(args []string) int {
	flags := flag.NewFlagSet("gpp clean", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := ".gpp"
	flags.StringVar(&output, "output", output, "directory for generated Go code")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return commandHelp("clean")
		}
		return reportError(err)
	}
	if flags.NArg() != 0 {
		return reportError(errors.New("clean does not accept source paths"))
	}
	clean, err := filepath.Abs(output)
	if err != nil {
		return reportError(err)
	}
	working, err := filepath.Abs(".")
	if err != nil {
		return reportError(err)
	}
	if clean == working || clean == string(filepath.Separator) {
		return reportError(fmt.Errorf("refusing to clean unsafe directory %q", output))
	}
	if output != ".gpp" {
		marker := filepath.Join(clean, ".gpp-generated")
		if _, err := os.Stat(marker); err != nil {
			if os.IsNotExist(err) {
				return reportError(fmt.Errorf("refusing to clean %q: it is not marked as Go++ compiler output", output))
			}
			return reportError(err)
		}
	}
	if err := os.RemoveAll(clean); err != nil {
		return reportError(err)
	}
	fmt.Printf("cleaned %s\n", output)
	return 0
}

func runFmt(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		return commandHelp("fmt")
	}
	if len(args) == 0 {
		args = []string{"."}
	}
	sources, err := discoverSources(args)
	if err != nil {
		return reportError(err)
	}
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return reportError(err)
		}
		formatted := compiler.FormatSource(string(data))
		if err := os.WriteFile(source, []byte(formatted), 0644); err != nil {
			return reportError(err)
		}
		fmt.Println(source)
	}
	return 0
}

func formatSource(source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	lines := strings.Split(source, "\n")
	indent := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			lines[index] = ""
			continue
		}
		if strings.HasPrefix(trimmed, "}") {
			indent--
			if indent < 0 {
				indent = 0
			}
		}
		lines[index] = strings.Repeat("\t", indent) + trimmed
		indent += braceDelta(trimmed)
		if indent < 0 {
			indent = 0
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func wantsHelp(args []string) bool {
	return len(args) == 1 && (args[0] == "-h" || args[0] == "--help")
}

func braceDelta(line string) int {
	open, close := 0, 0
	inString := false
	escaped := false
	for _, char := range line {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && inString {
			escaped = true
			continue
		}
		if char == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch char {
		case '{':
			open++
		case '}':
			close++
		}
	}
	return open - close
}

func runEnv(args []string) int {
	if len(args) > 0 {
		if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
			return commandHelp("env")
		}
		return reportError(errors.New("env does not accept arguments"))
	}
	goPath, goErr := findGo()
	version, versionErr := goVersion()
	cache := "<unavailable>"
	if directory, err := os.UserCacheDir(); err == nil {
		cache = filepath.Join(directory, "gpp")
	}
	if goErr != nil {
		goPath = "<not found>"
	}
	if versionErr != nil {
		version = "<unknown>"
	}
	fmt.Printf("GPP_VERSION=%q\nGPP_CACHE=%q\nGO=%q\nGO_VERSION=%q\nGOOS=%q\nGOARCH=%q\n",
		Version, cache, goPath, version, runtime.GOOS, runtime.GOARCH)
	if goPath != "<not found>" {
		if output, err := exec.Command(goPath, "env", "GOPATH", "GOROOT").Output(); err == nil {
			values := strings.Fields(string(output))
			if len(values) == 2 {
				fmt.Printf("GOPATH=%q\nGOROOT=%q\n", values[0], values[1])
			}
		}
	}
	return 0
}

func runDoctor(args []string) int {
	if len(args) > 0 {
		if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
			return commandHelp("doctor")
		}
		return reportError(errors.New("doctor does not accept arguments"))
	}
	fmt.Printf("Go++ %s\n", Version)
	failed := false
	goPath, err := findGo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %s\n", err)
		return 1
	}
	fmt.Printf("✓ Go found: %s\n", goPath)
	version, err := goVersion()
	if err != nil || !supportedGo(version) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "✗ Go %s is unsupported; Go 1.%d or newer is required\n", version, minimumGoMinor)
		}
		failed = true
	} else {
		fmt.Printf("✓ Go version supported: %s\n", version)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ cache directory unavailable: %v\n", err)
		failed = true
	} else {
		cache = filepath.Join(cache, "gpp")
		if err := os.MkdirAll(cache, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "✗ cache not writable: %v\n", err)
			failed = true
		} else {
			fmt.Printf("✓ cache writable: %s\n", cache)
		}
	}
	if !compiler.EmbeddedStdlibAvailable() {
		fmt.Fprintln(os.Stderr, "✗ embedded standard library unavailable")
		failed = true
	} else {
		fmt.Println("✓ embedded standard library available")
	}
	temporary, err := os.MkdirTemp("", "gpp-doctor-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ temporary build directory unavailable: %v\n", err)
		failed = true
	} else {
		defer os.RemoveAll(temporary)
		source := filepath.Join(temporary, "main.gpp")
		if err := os.WriteFile(source, []byte("func main() {}\n"), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "✗ test source could not be created: %v\n", err)
			failed = true
		} else if err := compiler.CompileFilesWithOptions([]string{source}, filepath.Join(temporary, ".gpp"), compiler.CompileOptions{ModulePath: "generated", NoStdlib: true, CleanOutput: true}); err != nil {
			fmt.Fprintf(os.Stderr, "✗ test compilation failed: %v\n", err)
			failed = true
		} else {
			fmt.Println("✓ test compilation succeeded")
		}
	}
	if failed {
		return 1
	}
	return 0
}

func runVersion(args []string) int {
	if len(args) > 0 {
		if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
			return commandHelp("version")
		}
		return reportError(errors.New("version does not accept arguments"))
	}
	version, err := goVersion()
	if err != nil {
		fmt.Printf("gpp %s\n", Version)
		return 0
	}
	fmt.Printf("gpp %s\ngo %s\n%s/%s\n", Version, version, runtime.GOOS, runtime.GOARCH)
	return 0
}

func reportError(err error) int {
	var backend backendDiagnosticError
	if errors.As(err, &backend) {
		fmt.Fprintln(os.Stderr, backend.Error())
		return 1
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func mustGetwd() string {
	working, err := os.Getwd()
	if err != nil {
		return "gpp-app"
	}
	return working
}
