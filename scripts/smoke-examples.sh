#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_root="$(mktemp -d "${TMPDIR:-/tmp}/gpp-example-smoke.XXXXXX")"
trap 'rm -rf "$temp_root"' EXIT
module_cache="$(go env GOMODCACHE)"

export GPP_CACHE="$temp_root/gpp-cache"
export GOCACHE="$temp_root/go-cache"
export GOMODCACHE="$module_cache"
export GIT_CONFIG_GLOBAL="$temp_root/gitconfig"
mkdir -p "$temp_root/bin"

# gpp init makes an initial commit, so give this isolated project a temporary
# identity without changing the user's Git configuration.
git config --global user.name "Go++ example smoke test"
git config --global user.email "gpp-smoke@example.invalid"

echo "Building Go++ compiler..."
(cd "$repo_root" && go build -o "$temp_root/bin/gpp" .)
gpp="$temp_root/bin/gpp"

project="$temp_root/project"
echo "Initializing temporary project..."
"$gpp" init -module example.com/gpp-example-smoke "$project"
cp -R "$repo_root/examples" "$project/examples"
cd "$project"

run_example() {
    local name="$1"
    shift
    echo "Running example: $name"
    "$gpp" run "$@" >"$temp_root/$name.log" 2>&1 || {
        cat "$temp_root/$name.log"
        echo "Example failed: $name" >&2
        return 1
    }
}

build_example() {
    local name="$1"
    shift
    echo "Building example: $name"
    "$gpp" build "$@" >"$temp_root/$name.log" 2>&1 || {
        cat "$temp_root/$name.log"
        echo "Example failed to build: $name" >&2
        return 1
    }
}

for source in examples/*.gpp; do
    example="$(basename "$source" .gpp)"
    case "$example" in
        foo.bar) continue ;;
        http|oauth|sse|todo-app)
            # These examples start listeners; compile and link them here.
            build_example "$example" "$source"
            ;;
        testing)
            echo "Running Go++ test example..."
            "$gpp" test "$source" >"$temp_root/testing.log" 2>&1 || {
                cat "$temp_root/testing.log"
                echo "Example tests failed" >&2
                exit 1
            }
            ;;
        foo) run_example "$example" "$source" examples/foo.bar.gpp ;;
        tpl) run_example "$example" "$source" examples/tpl_external.gpp.tpl ;;
        *) run_example "$example" "$source" ;;
    esac
done

run_example packages examples/packages/people.gpp examples/packages/main.gpp
run_example mixed-go-from-gpp examples/mixed/go_from_gpp
run_example mixed-go-imports-gpp -module generated examples/mixed/go_imports_gpp

echo "All Go++ examples passed."
