package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

type httpDocumentRoute struct {
	Method string
	Path   string
}

func validateHTTPDocumentation(program *Program, model *SemanticModel) error {
	for _, file := range program.Files {
		pkg := model.Packages[file.Package]
		if pkg == nil {
			continue
		}
		for _, declaration := range file.Decls {
			class, ok := declaration.(*ClassDecl)
			if !ok || !httpServerClass(pkg, class, map[*ClassDecl]bool{}) {
				continue
			}
			var openAPI, swagger *AnnotationUse
			for index := range class.Annotations {
				use := &class.Annotations[index]
				switch httpAnnotationKind(*use, pkg) {
				case "OpenAPI":
					openAPI = use
				case "Swagger":
					swagger = use
				}
			}
			if swagger == nil {
				continue
			}
			if openAPI == nil {
				return fmt.Errorf("%s: http.Swagger requires http.OpenAPI on the same server", annotationUseLocation(*swagger))
			}

			prefix := httpClassAnnotationPath(pkg, class, "Prefix", "")
			openAPIPath := joinHTTPPath(prefix, httpClassAnnotationPath(pkg, class, "OpenAPI", "/openapi.json"))
			swaggerPath := normalizeHTTPMountPath(joinHTTPPath(prefix, httpClassAnnotationPath(pkg, class, "Swagger", "/swagger")))
			if openAPIPath == swaggerPath || pathOwnedByHTTPMount(openAPIPath, swaggerPath) {
				return fmt.Errorf("%s: http.OpenAPI route %s conflicts with http.Swagger mount %s", swagger.SourceFile, openAPIPath, swaggerPath)
			}
			if pathOwnedByHTTPMount(swaggerPath, openAPIPath) {
				return fmt.Errorf("%s: http.Swagger route %s conflicts with http.OpenAPI", annotationUseLocation(*swagger), swaggerPath)
			}
			for _, route := range httpClassRoutes(pkg, class, prefix) {
				if route.Path == openAPIPath && route.Method == "GET" {
					return fmt.Errorf("%s: GET %s conflicts with http.OpenAPI", annotationUseLocation(*openAPI), openAPIPath)
				}
				if route.Path == swaggerPath && route.Method == "GET" {
					return fmt.Errorf("%s: GET %s conflicts with http.Swagger", annotationUseLocation(*swagger), swaggerPath)
				}
				if route.Path != swaggerPath && pathOwnedByHTTPMount(route.Path, swaggerPath) {
					return fmt.Errorf("%s: %s %s conflicts with http.Swagger mount %s", annotationUseLocation(*swagger), route.Method, route.Path, swaggerPath)
				}
			}
		}
	}
	return nil
}

func httpAnnotationKind(use AnnotationUse, pkg *PackageSymbols) string {
	name := use.Name
	last := name
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		last = name[dot+1:]
		prefix := name[:dot]
		if pkg.Imports[prefix] != "gpp/http" {
			if use.Declaration == nil || use.Declaration.Package != "gpp.http" {
				return ""
			}
		}
	} else if use.Declaration == nil || use.Declaration.Package != "gpp.http" {
		return ""
	}
	switch last {
	case "OpenAPI", "Swagger", "Prefix", "GET", "POST", "PUT", "PATCH", "DELETE":
		return last
	default:
		return ""
	}
}

func httpServerClass(pkg *PackageSymbols, class *ClassDecl, visiting map[*ClassDecl]bool) bool {
	if class == nil || visiting[class] {
		return false
	}
	visiting[class] = true
	for _, parent := range class.Parents {
		if parent == "http.Server" || parent == "gpp.http.Server" {
			return true
		}
		if imported := pkg.ImportedClasses[parent]; imported != nil && imported.Name == "Server" {
			return true
		}
		if local := pkg.Classes[parent]; local != nil && httpServerClass(pkg, local, visiting) {
			return true
		}
	}
	return false
}

func httpClassAnnotationPath(pkg *PackageSymbols, class *ClassDecl, name, fallback string) string {
	for index := range class.Annotations {
		use := class.Annotations[index]
		if httpAnnotationKind(use, pkg) != name {
			continue
		}
		if !use.HasArguments || strings.TrimSpace(use.Arguments) == "" {
			return fallback
		}
		args, err := splitTopLevel(use.Arguments, ',')
		if err != nil || len(args) == 0 {
			return fallback
		}
		value, err := strconv.Unquote(strings.TrimSpace(args[0]))
		if err != nil {
			return fallback
		}
		return value
	}
	return fallback
}

func httpClassRoutes(pkg *PackageSymbols, class *ClassDecl, prefix string) []httpDocumentRoute {
	var routes []httpDocumentRoute
	for _, method := range class.Methods {
		for _, use := range method.Annotations {
			kind := httpAnnotationKind(use, pkg)
			switch kind {
			case "GET", "POST", "PUT", "PATCH", "DELETE":
				path := httpUsePath(use)
				if path != "" {
					routes = append(routes, httpDocumentRoute{Method: kind, Path: joinHTTPPath(prefix, path)})
				}
			}
		}
	}
	return routes
}

func httpUsePath(use AnnotationUse) string {
	if !use.HasArguments || strings.TrimSpace(use.Arguments) == "" {
		return ""
	}
	args, err := splitTopLevel(use.Arguments, ',')
	if err != nil || len(args) == 0 {
		return ""
	}
	value, err := strconv.Unquote(strings.TrimSpace(args[0]))
	if err != nil {
		return ""
	}
	return value
}

func joinHTTPPath(prefix, route string) string {
	if prefix == "" {
		return route
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(route, "/")
}

func normalizeHTTPMountPath(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}

func pathOwnedByHTTPMount(path, mount string) bool {
	path = normalizeHTTPMountPath(path)
	mount = normalizeHTTPMountPath(mount)
	return path == mount || (mount != "/" && strings.HasPrefix(path, mount+"/"))
}
