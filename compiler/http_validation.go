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
			oauthUses := []*AnnotationUse{}
			for index := range class.Annotations {
				use := &class.Annotations[index]
				switch httpAnnotationKind(*use, pkg) {
				case "OpenAPI":
					openAPI = use
				case "Swagger":
					swagger = use
				case "OAuth":
					oauthUses = append(oauthUses, use)
				}
			}
			if len(oauthUses) > 0 {
				if !httpServerClass(pkg, class, map[*ClassDecl]bool{}) {
					return fmt.Errorf("%s: http.OAuth requires a class derived from http.Server", annotationUseLocation(*oauthUses[0]))
				}
				if err := validateHTTPOAuthAnnotations(pkg, class, oauthUses, prefixForHTTPClass(pkg, class)); err != nil {
					return err
				}
				if err := validateHTTPOAuthHooks(pkg, class); err != nil {
					return err
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
	case "OpenAPI", "Swagger", "OAuth", "Prefix", "GET", "POST", "PUT", "PATCH", "DELETE":
		return last
	default:
		return ""
	}
}

func validateHTTPOAuthAnnotations(pkg *PackageSymbols, class *ClassDecl, uses []*AnnotationUse, prefix string) error {
	seen := map[string]bool{}
	routes := httpClassRoutes(pkg, class, prefix)
	for _, use := range uses {
		name, err := httpOAuthStaticName(*use)
		if err != nil {
			return fmt.Errorf("%s: %w", annotationUseLocation(*use), err)
		}
		if seen[name] {
			return fmt.Errorf("%s: duplicate OAuth provider %s", annotationUseLocation(*use), name)
		}
		seen[name] = true
		loginPath := joinHTTPPath(prefix, "/auth/"+name)
		callbackPath := loginPath + "/callback"
		for _, route := range routes {
			if route.Method == "GET" && (route.Path == loginPath || route.Path == callbackPath) {
				return fmt.Errorf("%s: GET %s conflicts with generated OAuth route", annotationUseLocation(*use), route.Path)
			}
		}
	}
	return nil
}

func httpOAuthStaticName(use AnnotationUse) (string, error) {
	if !use.HasArguments || strings.TrimSpace(use.Arguments) == "" {
		return "", fmt.Errorf("http.OAuth requires a provider or explicit provider configuration")
	}
	args, err := splitTopLevel(use.Arguments, ',')
	if err != nil || len(args) == 0 {
		return "", fmt.Errorf("http.OAuth has invalid arguments")
	}
	first := strings.TrimSpace(args[0])
	if !strings.HasPrefix(first, "\"") && !strings.HasPrefix(first, "`") && strings.Contains(first, "OAuthProvider.") {
		member := first[strings.LastIndex(first, ".")+1:]
		providers := map[string]string{"Google": "google", "Facebook": "facebook", "GitHub": "github", "Microsoft": "microsoft", "Apple": "apple"}
		name, ok := providers[member]
		if !ok {
			return "", fmt.Errorf("unknown OAuth provider %s", member)
		}
		for index, argument := range args[1:] {
			if _, err := strconv.Unquote(strings.TrimSpace(argument)); err != nil {
				return "", fmt.Errorf("OAuth scope %d must be a string literal", index+1)
			}
		}
		return name, nil
	}
	if len(args) < 4 {
		return "", fmt.Errorf("explicit http.OAuth requires name, issuer URL, client ID environment name, and client secret environment name")
	}
	values := make([]string, 4)
	for index := range values {
		value, err := strconv.Unquote(strings.TrimSpace(args[index]))
		if err != nil {
			return "", fmt.Errorf("explicit http.OAuth argument %d must be a string literal", index+1)
		}
		values[index] = value
	}
	if !gppOAuthValidNameForCompiler(values[0]) {
		return "", fmt.Errorf("invalid OAuth provider name %q; use lowercase letters, numbers, and hyphens", values[0])
	}
	for index, argument := range args[4:] {
		if _, err := strconv.Unquote(strings.TrimSpace(argument)); err != nil {
			return "", fmt.Errorf("OAuth scope %d must be a string literal", index+1)
		}
	}
	return values[0], nil
}

func gppOAuthValidNameForCompiler(name string) bool {
	if name == "" {
		return false
	}
	for _, value := range name {
		if value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '-' {
			continue
		}
		return false
	}
	return true
}

func validateHTTPOAuthHooks(pkg *PackageSymbols, class *ClassDecl) error {
	for _, method := range class.Methods {
		if method.Name != "OAuthLogin" && method.Name != "OAuthError" {
			continue
		}
		parameters, err := parseParameterInfos(method.Parameters)
		if err != nil {
			return fmt.Errorf("%s: %s has invalid parameters: %w", classLocation(class), method.Name, err)
		}
		if method.Result != "" && strings.TrimSpace(method.Result) != "error" {
			return fmt.Errorf("%s: %s may return only error", classLocation(class), method.Name)
		}
		if method.Name == "OAuthError" {
			if len(parameters) != 3 || !httpOAuthTypeMatches(pkg, parameters[0].Type, "Context", true, false) || normalizeHTTPHookType(parameters[1].Type) != "string" || normalizeHTTPHookType(parameters[2].Type) != "error" {
				return fmt.Errorf("%s: OAuthError must have signature (ctx *http.Context, provider string, err error)", classLocation(class))
			}
			continue
		}
		if len(parameters) != 2 && len(parameters) != 3 {
			return fmt.Errorf("%s: OAuthLogin must have signature (ctx *http.Context, identity http.OAuthIdentity[, token http.OAuthToken])", classLocation(class))
		}
		if !httpOAuthTypeMatches(pkg, parameters[0].Type, "Context", true, false) || !httpOAuthTypeMatches(pkg, parameters[1].Type, "OAuthIdentity", false, true) {
			return fmt.Errorf("%s: OAuthLogin has invalid signature", classLocation(class))
		}
		if len(parameters) == 3 && !httpOAuthTypeMatches(pkg, parameters[2].Type, "OAuthToken", false, true) {
			return fmt.Errorf("%s: OAuthLogin token parameter must be http.OAuthToken", classLocation(class))
		}
	}
	return nil
}

func normalizeHTTPHookType(value string) string {
	return strings.Join(strings.Fields(value), "")
}

func httpOAuthTypeMatches(pkg *PackageSymbols, value, name string, pointer, allowPointer bool) bool {
	typeName := normalizeHTTPHookType(value)
	if pointer && !strings.HasPrefix(typeName, "*") {
		return false
	}
	if !pointer && strings.HasPrefix(typeName, "*") {
		if !allowPointer {
			return false
		}
		typeName = strings.TrimPrefix(typeName, "*")
	}
	if pointer {
		typeName = strings.TrimPrefix(typeName, "*")
	}
	if typeName == "gpp.http."+name {
		return true
	}
	for alias, importPath := range pkg.Imports {
		if importPath == "gpp/http" && typeName == alias+"."+name {
			return true
		}
	}
	return false
}

func prefixForHTTPClass(pkg *PackageSymbols, class *ClassDecl) string {
	return httpClassAnnotationPath(pkg, class, "Prefix", "")
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
