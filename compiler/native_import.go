package compiler

import (
	"go/importer"
	"go/types"
	"sync"
)

var nativePackageCache = struct {
	sync.Mutex
	packages map[string]*types.Package
	errors   map[string]error
}{
	packages: map[string]*types.Package{},
	errors:   map[string]error{},
}

// importNativePackage keeps native type resolution from repeatedly decoding
// the same export data while a generated Go++ package is being emitted.
// Standard-library sources can contain many native selectors, and the emitter
// may inspect each selector more than once during overload and polymorphism
// passes.
func importNativePackage(importPath string) (*types.Package, error) {
	nativePackageCache.Lock()
	if pkg, ok := nativePackageCache.packages[importPath]; ok {
		nativePackageCache.Unlock()
		return pkg, nil
	}
	if err, ok := nativePackageCache.errors[importPath]; ok {
		nativePackageCache.Unlock()
		return nil, err
	}
	nativePackageCache.Unlock()

	pkg, err := importer.Default().Import(importPath)
	nativePackageCache.Lock()
	defer nativePackageCache.Unlock()
	if err != nil {
		nativePackageCache.errors[importPath] = err
		return nil, err
	}
	nativePackageCache.packages[importPath] = pkg
	return pkg, nil
}
