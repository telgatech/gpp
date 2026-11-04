package command

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"hash"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/telgatech/gpp/compiler"
)

// Bump this when compiler output or cache input discovery changes.
const buildCacheSchema = 5

var errBuildCacheUncacheable = errors.New("build inputs cannot be fingerprinted safely")

const (
	buildCacheManifestName    = ".gpp-build-cache.json"
	moduleCacheManifestName   = ".gpp-mod-tidy-cache"
	artifactCacheManifestName = ".gpp-artifact-cache.json"
	nativeGoFilesManifestName = ".gpp-native-files.json"
)

type buildCacheManifest struct {
	Schema       int      `json:"schema"`
	Key          string   `json:"key"`
	OutputDigest string   `json:"output_digest"`
	Warnings     []string `json:"warnings,omitempty"`
}

type artifactCacheManifest struct {
	Key     string `json:"key"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

type nativeGoFilesManifest struct {
	Files []string `json:"files"`
}

type buildCacheState struct {
	Key  string
	Hit  bool
	Warn []string
}

func buildInputKey(sources []string, options compileFlags, selections ...testSelectionResult) (string, error) {
	h := sha256.New()
	writeHashPart(h, fmt.Sprintf("schema=%d", buildCacheSchema))
	writeHashPart(h, "compiler="+Version)
	writeHashPart(h, "module="+options.module)
	writeHashPart(h, fmt.Sprintf("no-prelude=%t", options.noPrelude))
	writeHashPart(h, fmt.Sprintf("no-stdlib=%t", options.noStdlib))
	writeHashPart(h, fmt.Sprintf("development=%t", options.development))
	writeHashPart(h, "GOOS="+envOrDefault("GOOS", runtime.GOOS))
	writeHashPart(h, "GOARCH="+envOrDefault("GOARCH", runtime.GOARCH))
	writeHashPart(h, "CGO_ENABLED="+os.Getenv("CGO_ENABLED"))
	writeHashPart(h, "GOFLAGS="+os.Getenv("GOFLAGS"))
	writeHashPart(h, "GOTOOLCHAIN="+os.Getenv("GOTOOLCHAIN"))
	if working, err := os.Getwd(); err == nil {
		writeHashPart(h, "cwd="+working)
	}
	for _, selection := range selections {
		writeHashPart(h, fmt.Sprintf("tests=%#v", selection))
	}

	compiled := make(map[string]bool, len(sources))
	for _, source := range sources {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return "", err
		}
		compiled[filepath.Clean(absolute)] = true
		if err := hashFile(h, absolute); err != nil {
			return "", err
		}
		if err := hashSourceEmbeds(h, absolute, options.output); err != nil {
			if errors.Is(err, errBuildCacheUncacheable) {
				return "", nil
			}
			return "", err
		}
	}

	directories := map[string]bool{}
	for _, source := range sources {
		directories[filepath.Dir(source)] = true
	}
	dirNames := make([]string, 0, len(directories))
	for directory := range directories {
		dirNames = append(dirNames, directory)
	}
	sort.Strings(dirNames)
	for _, directory := range dirNames {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			absolute, err := filepath.Abs(path)
			if err != nil {
				return "", err
			}
			if compiled[filepath.Clean(absolute)] {
				continue
			}
			matches, err := nativeGoFileMatchesBuild(directory, entry.Name())
			if err != nil {
				return "", fmt.Errorf("inspect native Go file %s: %w", path, err)
			}
			if matches {
				if err := hashFile(h, absolute); err != nil {
					return "", err
				}
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashSourceEmbeds(h hash.Hash, source, parseCacheDir string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	// Parsing every source on a cache hit would erase much of the benefit.
	// Files without the embed keyword cannot contribute external embed inputs.
	if !strings.Contains(string(data), "embed") {
		return nil
	}
	file, err := compiler.ParseFileWithCache(filepath.Base(source), source, string(data), parseCacheDir)
	if err != nil {
		// Leave normal compilation responsible for reporting source errors, and
		// don't cache when embed inputs cannot be discovered safely.
		return errBuildCacheUncacheable
	}
	root := nearestModuleRoot(filepath.Dir(source))
	if root == "" {
		root = filepath.Dir(source)
	}
	for _, declaration := range file.Decls {
		embed, ok := declaration.(*compiler.EmbedDecl)
		if !ok {
			continue
		}
		for _, item := range embed.Entries {
			if strings.ContainsAny(item.Path, "*?[]\\") || filepath.IsAbs(filepath.FromSlash(item.Path)) {
				continue
			}
			cleanPath := filepath.Clean(filepath.FromSlash(strings.TrimSuffix(item.Path, "/")))
			if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
				continue
			}
			path := filepath.Join(root, cleanPath)
			writeHashPart(h, "embed="+filepath.ToSlash(path))
			if item.Directory {
				if err := hashDirectory(h, path); err != nil {
					if os.IsNotExist(err) {
						writeHashPart(h, "embed-missing")
						continue
					}
					return err
				}
			} else if err := hashFile(h, path); err != nil {
				if os.IsNotExist(err) {
					writeHashPart(h, "embed-missing")
					continue
				}
				return err
			}
		}
	}
	return nil
}

func hashDirectory(h hash.Hash, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		return hashFile(h, path)
	})
}

func hashFile(h hash.Hash, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	writeHashPart(h, "file="+filepath.ToSlash(path))
	writeHashPart(h, string(data))
	return nil
}

func writeHashPart(h hash.Hash, value string) {
	_, _ = fmt.Fprintf(h, "%d:", len(value))
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func loadBuildCache(output, key string) (buildCacheManifest, bool, error) {
	path := filepath.Join(output, buildCacheManifestName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return buildCacheManifest{}, false, nil
	}
	if err != nil {
		return buildCacheManifest{}, false, err
	}
	var manifest buildCacheManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Schema != buildCacheSchema || manifest.Key != key {
		return buildCacheManifest{}, false, nil
	}
	digest, err := generatedOutputDigest(output)
	if err != nil {
		return buildCacheManifest{}, false, err
	}
	if digest != manifest.OutputDigest {
		return buildCacheManifest{}, false, nil
	}
	return manifest, true, nil
}

func writeBuildCache(output, key string, warnings []unusedLocalWarning) error {
	digest, err := generatedOutputDigest(output)
	if err != nil {
		return err
	}
	manifest := buildCacheManifest{Schema: buildCacheSchema, Key: key, OutputDigest: digest}
	for _, warning := range warnings {
		manifest.Warnings = append(manifest.Warnings, warning.String())
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	temporary := filepath.Join(output, buildCacheManifestName+".tmp")
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(output, buildCacheManifestName))
}

func loadNativeGoFilesManifest(output string) (map[string]bool, error) {
	path := filepath.Join(output, nativeGoFilesManifestName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest nativeGoFilesManifest
	if json.Unmarshal(data, &manifest) != nil {
		return map[string]bool{}, nil
	}
	files := make(map[string]bool, len(manifest.Files))
	for _, name := range manifest.Files {
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			continue
		}
		files[filepath.ToSlash(clean)] = true
	}
	return files, nil
}

func writeNativeGoFilesManifest(output string, files map[string]bool) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	data, err := json.Marshal(nativeGoFilesManifest{Files: names})
	if err != nil {
		return err
	}
	temporary := filepath.Join(output, nativeGoFilesManifestName+".tmp")
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(output, nativeGoFilesManifestName))
}

func removeEmptyCacheParents(directory, output string) {
	root, err := filepath.Abs(output)
	if err != nil {
		return
	}
	current, err := filepath.Abs(directory)
	for err == nil && current != root && isWithin(root, current) {
		if err := os.Remove(current); err != nil {
			return
		}
		current = filepath.Dir(current)
	}
}

func generatedOutputDigest(output string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(output, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if name == "go.mod" || name == "go.sum" || name == buildCacheManifestName || name == buildCacheManifestName+".tmp" || name == moduleCacheManifestName || name == artifactCacheManifestName {
			return nil
		}
		return hashFile(h, path)
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeModuleCacheKey(output, key string) error {
	return os.WriteFile(filepath.Join(output, moduleCacheManifestName), []byte(key), 0644)
}

func runGoModTidyCached(output, inputKey string) error {
	if inputKey == "" {
		return runGoCommand(output, "mod", "tidy")
	}
	goIdentity, err := currentGoBuildIdentity()
	if err != nil {
		return err
	}
	key, err := moduleCacheKey(output, goIdentity)
	if err != nil {
		return err
	}
	if moduleCacheHit(output, key) {
		return nil
	}
	if err := runGoCommand(output, "mod", "tidy"); err != nil {
		return err
	}
	key, err = moduleCacheKey(output, goIdentity)
	if err != nil {
		return err
	}
	return writeModuleCacheKey(output, key)
}

func moduleCacheHit(output, key string) bool {
	data, err := os.ReadFile(filepath.Join(output, moduleCacheManifestName))
	return err == nil && string(data) == key
}

func buildArtifactKey(inputKey, output, binaryPath, goIdentity string) (string, error) {
	h := sha256.New()
	writeHashPart(h, "input="+inputKey)
	writeHashPart(h, "binary="+filepath.Clean(binaryPath))
	writeHashPart(h, "go="+goIdentity)
	writeHashPart(h, "GOOS="+envOrDefault("GOOS", runtime.GOOS))
	writeHashPart(h, "GOARCH="+envOrDefault("GOARCH", runtime.GOARCH))
	writeHashPart(h, "CGO_ENABLED="+os.Getenv("CGO_ENABLED"))
	writeHashPart(h, "GOFLAGS="+os.Getenv("GOFLAGS"))
	for _, name := range []string{"go.mod", "go.sum"} {
		path := filepath.Join(output, name)
		if _, err := os.Stat(path); err == nil {
			if err := hashFile(h, path); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func cachedArtifact(output, binaryPath, key string) bool {
	data, err := os.ReadFile(filepath.Join(output, artifactCacheManifestName))
	if err != nil {
		return false
	}
	var manifest artifactCacheManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Key != key {
		return false
	}
	info, err := os.Stat(binaryPath)
	return err == nil && info.Mode().IsRegular() && info.Size() == manifest.Size && info.ModTime().UnixNano() == manifest.ModTime
}

func writeArtifactCache(output, binaryPath, key string) error {
	info, err := os.Stat(binaryPath)
	if err != nil {
		return err
	}
	data, err := json.Marshal(artifactCacheManifest{Key: key, Size: info.Size(), ModTime: info.ModTime().UnixNano()})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, artifactCacheManifestName), data, 0644)
}

func moduleCacheKey(output, goIdentity string) (string, error) {
	h := sha256.New()
	writeHashPart(h, "go="+goIdentity)
	imports, err := generatedImportKey(output)
	if err != nil {
		return "", err
	}
	writeHashPart(h, "imports="+imports)
	for _, name := range []string{"go.mod", "go.sum"} {
		path := filepath.Join(output, name)
		if _, err := os.Stat(path); err == nil {
			if err := hashFile(h, path); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func generatedImportKey(output string) (string, error) {
	h := sha256.New()
	imports := map[string]bool{}
	err := filepath.WalkDir(output, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, item := range file.Imports {
			importPath, err := strconv.Unquote(item.Path.Value)
			if err != nil {
				return err
			}
			imports[importPath] = true
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(imports))
	for name := range imports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writeHashPart(h, name)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func currentGoBuildIdentity() (string, error) {
	goPath, err := findGo()
	if err != nil {
		return "", err
	}
	output, err := exec.Command(goPath, "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT", "GOAMD64", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOTOOLCHAIN", "GOVERSION").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("get Go build settings: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
