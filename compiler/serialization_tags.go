package compiler

import (
	"strconv"
	"strings"
)

func serializationFieldTag(uses []AnnotationUse) string {
	name := ""
	ignore := false
	omitEmpty := false
	for _, use := range uses {
		switch serializationAnnotationKind(use) {
		case "Name":
			args, err := splitTopLevel(use.Arguments, ',')
			if err == nil && len(args) == 1 {
				if value, err := strconv.Unquote(strings.TrimSpace(args[0])); err == nil {
					name = value
				}
			}
		case "Ignore":
			ignore = true
		case "OmitEmpty":
			omitEmpty = true
		}
	}
	if !ignore && name == "" && !omitEmpty {
		return ""
	}
	if ignore {
		return " `json:\"-\" yaml:\"-\"`"
	}
	options := name
	if omitEmpty {
		options += ",omitempty"
	}
	return " `json:" + strconv.Quote(options) + " yaml:" + strconv.Quote(options) + "`"
}

func serializationAnnotationKind(use AnnotationUse) string {
	if use.Declaration != nil && strings.HasPrefix(annotationFullName(use.Declaration), serializationPackage+".") {
		switch use.Declaration.Name {
		case "Name", "Ignore", "OmitEmpty":
			return use.Declaration.Name
		}
	}
	for _, name := range []string{"Name", "Ignore", "OmitEmpty"} {
		if use.Name == "encoding."+name || use.Name == serializationPackage+"."+name {
			return name
		}
	}
	return ""
}
