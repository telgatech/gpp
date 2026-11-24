package main

import (
	"errors"
	"strings"
)

func NativeGreeting(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	return "Hello, " + name + " from native Go", nil
}

func NativeAdd(left int, right int) int {
	return left + right
}
