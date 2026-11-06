package compiler

import (
	"errors"
	"fmt"
)

// DiagnosticError carries a source location separately from the diagnostic
// message so each compiler frontend and output surface can share one format.
type DiagnosticError struct {
	File  string
	Span  Span
	Cause error
}

func (diagnostic DiagnosticError) Error() string {
	message := errorMessage(diagnostic.Cause)
	if diagnostic.Span.Line <= 0 {
		return fmt.Sprintf(
			"internal compiler error: source diagnostic has incomplete position (file=%q, line=%d): %s",
			diagnostic.File,
			diagnostic.Span.Line,
			message,
		)
	}
	if diagnostic.File == "" {
		if diagnostic.Span.Column > 0 {
			return fmt.Sprintf("%d:%d: %s", diagnostic.Span.Line, diagnostic.Span.Column, message)
		}
		return fmt.Sprintf("%d: %s", diagnostic.Span.Line, message)
	}
	if diagnostic.Span.Column <= 0 {
		return fmt.Sprintf("%s:%d: %s", diagnostic.File, diagnostic.Span.Line, message)
	}
	return fmt.Sprintf("%s:%d:%d: %s", diagnostic.File, diagnostic.Span.Line, diagnostic.Span.Column, message)
}

func (diagnostic DiagnosticError) Unwrap() error { return diagnostic.Cause }

// DiagnosticPosition extracts a compiler diagnostic's source location without
// parsing its user-facing text. LSP and command output should use this API.
type DiagnosticPosition struct {
	File    string
	Span    Span
	Message string
}

func PositionOfDiagnostic(err error) (DiagnosticPosition, bool) {
	var diagnostic DiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.File == "" || diagnostic.Span.Line <= 0 {
		return DiagnosticPosition{}, false
	}
	return DiagnosticPosition{File: diagnostic.File, Span: diagnostic.Span, Message: errorMessage(diagnostic.Cause)}, true
}

// FileError adds file context to errors that do not describe a source location,
// such as missing embedded assets. It is deliberately distinct from a source
// diagnostic, which must always carry a complete position.
type FileError struct {
	File  string
	Cause error
}

func (diagnostic FileError) Error() string {
	if diagnostic.File == "" {
		return errorMessage(diagnostic.Cause)
	}
	return fmt.Sprintf("%s: %s", diagnostic.File, errorMessage(diagnostic.Cause))
}

func (diagnostic FileError) Unwrap() error { return diagnostic.Cause }

func errorMessage(err error) string {
	if err == nil {
		return "compiler diagnostic"
	}
	return err.Error()
}

// FormatError is the CLI's single formatter for compiler and tool errors.
func FormatError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func completeSpan(span Span, fallbackLine int) Span {
	if span.Line <= 0 {
		span.Line = fallbackLine
	}
	if span.Column <= 0 && span.Line > 0 {
		span.Column = 1
	}
	return span
}

func diagnosticAt(file string, span Span, err error) error {
	if err == nil {
		return nil
	}
	var located DiagnosticError
	if errors.As(err, &located) {
		if located.File == "" {
			located.File = file
		}
		if located.Span == (Span{}) {
			located.Span.Start = span.Start
			located.Span.End = span.End
		}
		if located.Span.Line <= 0 {
			located.Span.Line = span.Line
		}
		if located.Span.Column <= 0 {
			located.Span.Column = span.Column
		}
		return located
	}
	if span.Line <= 0 || span.Column <= 0 {
		if file == "" {
			return err
		}
		return FileError{File: file, Cause: err}
	}
	return DiagnosticError{File: file, Span: span, Cause: err}
}

// NewDiagnosticError creates a diagnostic for compiler, LSP, and toolchain
// diagnostics that need to share the compiler's source-location formatter.
func NewDiagnosticError(file string, span Span, cause error) error {
	return diagnosticAt(file, span, cause)
}

func rebaseDiagnosticToSource(err error, file string, base Span) error {
	var diagnostic DiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.File != "" || base.Line <= 0 {
		return err
	}
	span := diagnostic.Span
	localLine := span.Line
	span.Line += base.Line - 1
	if localLine == 1 && base.Column > 0 {
		span.Column += base.Column - 1
	}
	span.Start += base.Start
	span.End += base.Start
	return diagnosticAt(file, span, diagnostic.Cause)
}

func sourceLineError(span Span, err error) error {
	if span.Line <= 0 {
		return err
	}
	if span.Column <= 0 {
		span.Column = 1
	}
	return diagnosticAt("", span, err)
}

func annotationDiagnostic(use AnnotationUse, err error) error {
	span := use.SpanValue
	if span.Line == 0 || span.Column == 0 {
		span = Span{Line: use.SourceLine, Column: 1}
	}
	return diagnosticAt(use.SourceFile, span, err)
}

func classDiagnostic(class *ClassDecl, err error) error {
	if class == nil {
		return err
	}
	span := completeSpan(class.Span(), class.SourceLine)
	return diagnosticAt(class.SourceFile, span, err)
}

func classDiagnosticf(class *ClassDecl, format string, args ...any) error {
	return classDiagnostic(class, fmt.Errorf(format, args...))
}

func methodDiagnosticf(class *ClassDecl, method *Method, format string, args ...any) error {
	if method != nil && method.Span().Line > 0 {
		file := ""
		if class != nil {
			file = class.SourceFile
		}
		return diagnosticAt(file, completeSpan(method.Span(), 0), fmt.Errorf(format, args...))
	}
	return classDiagnosticf(class, format, args...)
}

func fieldDiagnosticf(class *ClassDecl, field *Field, format string, args ...any) error {
	if field != nil && field.Span().Line > 0 {
		file := ""
		if class != nil {
			file = class.SourceFile
		}
		return diagnosticAt(file, completeSpan(field.Span(), 0), fmt.Errorf(format, args...))
	}
	return classDiagnosticf(class, format, args...)
}

func templateDiagnostic(template *TemplateDecl, err error) error {
	if template == nil {
		return err
	}
	span := completeSpan(template.Span(), template.SourceLine)
	return diagnosticAt(template.SourceFile, span, err)
}

func templateDiagnosticf(template *TemplateDecl, format string, args ...any) error {
	return templateDiagnostic(template, fmt.Errorf(format, args...))
}

func embedDiagnostic(embed *EmbedDecl, err error) error {
	if embed == nil {
		return err
	}
	span := completeSpan(embed.Span(), embed.SourceLine)
	return diagnosticAt(embed.SourceFile, span, err)
}

func extensionDiagnosticf(extension *ExtendDecl, format string, args ...any) error {
	if extension == nil {
		return fmt.Errorf(format, args...)
	}
	span := completeSpan(extension.Span(), extension.SourceLine)
	return diagnosticAt(extension.SourceFile, span, fmt.Errorf(format, args...))
}

func annotationDeclarationDiagnostic(declaration *AnnotationDecl, err error) error {
	if declaration == nil {
		return err
	}
	span := completeSpan(declaration.Span(), declaration.SourceLine)
	return diagnosticAt(declaration.SourceFile, span, err)
}

func embedEntryDiagnostic(entry EmbedEntry, err error) error {
	span := entry.SpanValue
	if span.Line == 0 || span.Column == 0 {
		span = Span{Line: entry.SourceLine, Column: 1}
	}
	return diagnosticAt(entry.SourceFile, span, err)
}

func isSourceLineDiagnostic(err error) bool {
	_, ok := PositionOfDiagnostic(err)
	return ok
}
