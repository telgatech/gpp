Go++ Feature Spec: Versioned Imports + Automatic Go Module Resolution

Goal
Allow Go++ source files to declare external Go dependencies directly in import paths, including optional versions.

The compiler should automatically resolve/fetch external dependencies during build while still using the normal Go module system underneath.

Core syntax

Standard library import:

import "fmt"

Unversioned external import:

import "github.com/jmoiron/sqlx"

Versioned external import:

import "github.com/jmoiron/sqlx@v1.4.0"

Aliased versioned import:

import db "github.com/jmoiron/sqlx@v1.4.0"

Grouped imports:

import (
    "fmt"
    "net/http"
    db "github.com/jmoiron/sqlx@v1.4.0"
    "github.com/google/uuid@v1.6.0"
)

Semantics

1. Go++ import syntax extends normal Go imports by allowing a final:

   @version

2. The `@version` component is Go++-only dependency metadata.

3. Generated Go source must strip the version suffix.

Example Go++:

import "github.com/jmoiron/sqlx@v1.4.0"

Generated Go:

import "github.com/jmoiron/sqlx"

4. Dependency fetching/version resolution must use Go modules.

Do NOT create a separate Go++ package registry or dependency manager.

Underlying artifacts should remain normal:

go.mod
go.sum

5. Standard-library imports require no module resolution.

Examples:

"fmt"
"net/http"
"database/sql"
"encoding/json"

6. External imports without a version are valid:

import "github.com/google/uuid"

The compiler may resolve them using normal Go module rules.

For reproducible builds, once resolved, the selected version should be recorded in go.mod/go.sum.

7. Explicit versions are preferred when supplied:

import "github.com/google/uuid@v1.6.0"

The compiler should ensure the requested module version is present in go.mod.

8. Major-version module paths must work naturally.

Example:

import "github.com/foo/bar/v2@v2.3.1"

Interpret as:

Go import path:
github.com/foo/bar/v2

Requested module version:
v2.3.1

The `/v2` is part of the normal Go module path.
The final `@v2.3.1` is Go++ version metadata.

9. Determine the version separator using the final `@` in the import string.

Examples:

"github.com/foo/bar@v1.2.3"

path:
github.com/foo/bar

version:
v1.2.3

"github.com/foo/bar/v2@v2.4.0"

path:
github.com/foo/bar/v2

version:
v2.4.0

10. Initially support exact Go module versions only.

Examples:

@v1.4.0
@v2.3.1

Optional future support:

@latest
@v2
version ranges

Do NOT implement ranges in v1 unless already trivial.

Module declaration

Support a project/module declaration such as:

module github.com/telga/example

package main

The module name describes the generated Go module.

The compiler should generate or maintain:

module github.com/telga/example

inside go.mod.

Module declarations are project-level metadata.

Do not require the same module declaration in every source file.

Recommended rule:

- one module declaration per project
- typically located in the project/root source file
- multiple identical declarations may optionally be tolerated
- conflicting module declarations are compile errors

Example:

module github.com/telga/example

package main

import (
    "fmt"
    "github.com/google/uuid@v1.6.0"
)

func main() {
    fmt.Println(uuid.New())
}

Build behavior

`gpp build` should conceptually perform:

1. Parse all Go++ files.
2. Determine module name.
3. Collect imports.
4. Separate standard-library imports from external module imports.
5. Collect explicit version requirements.
6. Detect dependency conflicts.
7. Generate/update go.mod.
8. Resolve/download missing dependencies using normal Go tooling.
9. Generate Go source with version suffixes removed.
10. Run normal Go compilation.

Equivalent underlying operations may include:

go mod init
go get module@version
go mod download
go mod tidy
go build

The compiler may invoke Go tooling rather than reimplementing Go's module resolver.

Important:
dependency fetching should occur during explicit build/dependency commands, not merely when parsing a file or opening it in an editor.

Version conflicts

If the same module is explicitly requested at incompatible exact versions, produce a Go++ compile/build error.

Example:

a.gpp:

import "github.com/foo/bar@v1.2.0"

b.gpp:

import "github.com/foo/bar@v1.4.0"

Error:

conflicting dependency versions for github.com/foo/bar:

    a.gpp requests v1.2.0
    b.gpp requests v1.4.0

Do not silently pick one version in v1.

Identical explicit versions are valid.

Example:

a.gpp:
import "github.com/foo/bar@v1.4.0"

b.gpp:
import "github.com/foo/bar@v1.4.0"

valid.

Unversioned + versioned

Example:

a.gpp:
import "github.com/foo/bar"

b.gpp:
import "github.com/foo/bar@v1.4.0"

Treat the explicit version as the project requirement.

The unversioned import simply means the source file does not impose an additional version constraint.

Aliases

Preserve ordinary Go aliases.

Go++:

import sqlx "github.com/jmoiron/sqlx@v1.4.0"

Generated Go:

import sqlx "github.com/jmoiron/sqlx"

Dot imports and blank imports should remain compatible if Go++ already supports normal Go syntax:

import . "github.com/foo/bar@v1.2.0"
import _ "github.com/lib/pq@v1.10.9"

Generated Go:

import . "github.com/foo/bar"
import _ "github.com/lib/pq"

AST representation

An import node should retain both source and resolved information.

Conceptually:

type ImportDecl struct {
    Alias       string
    SourcePath  string
    ImportPath  string
    Version     string
}

Example source:

db "github.com/jmoiron/sqlx@v1.4.0"

AST:

Alias:
db

SourcePath:
github.com/jmoiron/sqlx@v1.4.0

ImportPath:
github.com/jmoiron/sqlx

Version:
v1.4.0

For:

"fmt"

AST:

ImportPath:
fmt

Version:
""

Parser

Extend ordinary Go import parsing so string import paths may optionally end with:

@<version>

Do not modify normal Go import-path contents before semantic dependency processing.

Dependency analysis

Build a project-level dependency table keyed by module/import path.

Example:

github.com/jmoiron/sqlx
    requestedVersion: v1.4.0
    files:
        db.gpp
        users.gpp

github.com/google/uuid
    requestedVersion: v1.6.0
    files:
        main.gpp

The compiler should use this table to:

- detect version conflicts
- generate module requirements
- decide what must be downloaded
- produce useful diagnostics

Go module source of truth

The Go++ source specifies requested versions.

go.mod/go.sum are generated/managed compatibility artifacts for the Go toolchain.

Do not expose a separate dependency format unless a later feature requires one.

Generated Go

Example Go++:

module github.com/telga/app

package main

import (
    "fmt"
    "github.com/google/uuid@v1.6.0"
)

func main() {
    fmt.Println(uuid.New())
}

Generated Go:

package main

import (
    "fmt"
    "github.com/google/uuid"
)

func main() {
    fmt.Println(uuid.New())
}

Generated go.mod:

module github.com/telga/app

go <current-supported-go-version>

require github.com/google/uuid v1.6.0

Interop requirement

The generated project must remain a normal valid Go module.

A user should be able to inspect the generated source and run ordinary Go tooling against it.

Do not invent wrappers around imported packages.

Imported Go packages must remain directly usable exactly as they are in Go.

Diagnostics

Examples:

invalid dependency version:

import "github.com/foo/bar@banana"

error:
invalid Go module version "banana"

conflicting versions:

github.com/foo/bar:
    v1.2.0 requested by a.gpp
    v1.4.0 requested by b.gpp

missing module declaration, if one is required for project builds:

error:
project module name is not declared

For single-file/temporary builds, the compiler may synthesize an internal temporary module instead.

Suggested implementation order

1. Extend ImportDecl with Version.
2. Parse final @version from imports.
3. Strip version before Go emission.
4. Collect project dependency requirements.
5. Detect explicit version conflicts.
6. Parse project-level `module` declaration.
7. Generate/update go.mod.
8. Invoke Go module resolution/download.
9. Run generated Go build.
10. Add aliases/grouped imports.
11. Add cross-file dependency tests.
12. Add temporary-module behavior for standalone files.

Tests

Stdlib:

import "fmt"

Must emit unchanged and perform no download.

Versioned dependency:

import "github.com/google/uuid@v1.6.0"

Must emit:

import "github.com/google/uuid"

and add/download v1.6.0.

Major version:

import "github.com/foo/bar/v2@v2.3.1"

Must preserve:

github.com/foo/bar/v2

as the generated import path.

Alias:

import db "github.com/jmoiron/sqlx@v1.4.0"

Must preserve alias `db`.

Conflict:

two files explicitly requiring different versions of the same dependency must fail.

Unversioned plus versioned:

import "github.com/foo/bar"
import "github.com/foo/bar@v1.3.0"

Must resolve to explicit v1.3.0.

Design principle

Go++ owns the convenient dependency syntax.

Go owns dependency resolution, package compatibility, compilation, and the ecosystem.

The user should be able to write:

import "github.com/foo/bar@v1.2.3"

and otherwise use the package exactly as if it had been imported normally in Go.
