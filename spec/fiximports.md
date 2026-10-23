# Go++ `gpp fiximports` Command Specification

## 1. Purpose

`gpp fiximports` finds source-level qualified names that match a unique package
and exported package member available to the current Go module, then adds the
corresponding imports. It is a semantic source fix and remains separate from
`gpp fmt`.

```sh
gpp fiximports
gpp fiximports main.gpp
gpp fiximports ./...
gpp fiximports --check ./...
gpp fiximports --stdout main.gpp
```

With no path, the command scans the current project. Directory discovery uses
the same Go++ source extensions and ignored directories as `gpp fmt`.

## 2. Resolution

The command considers packages listed by the active Go toolchain and current
module, along with the bundled Go++ standard library. For each qualified use,
it selects a unique matching package by this priority:

1. bundled Go++ standard-library packages;
2. Go standard-library packages;
3. other packages available to the current module.

The receiver must not be a name declared in the source file, and the selected
package must export the selected member. If multiple packages at the same
priority match one member, that use is left unchanged.

The command MUST leave a receiver unchanged when no package matches or when
more than one package matches. It MUST NOT guess between package paths. The
command does not add imports for unqualified identifiers.

Package resolution uses the local Go toolchain and module state. It does not
download dependencies or update `go.mod` and `go.sum`. A Go dependency must
already be available to the module before it can be selected.

## 3. Import edits

Imports are inserted as a new grouped import declaration after the existing
imports, or after the package clause when the file has no imports. New paths
are sorted lexically. If source uses require both a Go++ package and a Go
standard-library package with the same package name, the Go++ package keeps
the natural name and the standard-library package receives a `std` prefix
(for example, `http` and `stdhttp`). The affected standard-library selectors
are rewritten to the prefixed alias. This also applies when the colliding
Go++ package is already imported. A source receiver already written with the
`std` prefix explicitly requests a matching Go standard-library package.

Existing import paths and comments MUST be preserved. An existing standard
import may have its alias changed when a name clash requires it. Other source
outside import alias and selector edits MUST be preserved.

The command MUST be idempotent: a second invocation after successful fixes
does not add duplicate imports.

## 4. Modes

Normal mode writes only changed files and uses the command's atomic file-write
behavior. `--check` reports files for which imports or aliases need fixes,
does not modify them, and exits non-zero when any such file is found.
`--stdout` writes the result for exactly one source file and does not modify
it. `--check` and `--stdout` cannot be combined.

Malformed Go++ input is reported and left untouched. Ambiguous or unavailable
package matches are not errors and remain unchanged.

## 5. Relationship to formatting

`gpp fmt` remains responsible for canonical layout. `gpp fiximports` adds
resolvable missing imports and applies the alias and selector edits required
to resolve package-name clashes. It does not format source, remove unused
imports, or rename symbols.
