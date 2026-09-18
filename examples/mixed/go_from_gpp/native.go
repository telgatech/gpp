package main

import "strings"

func NativeGreeting(name string) string {
	return "Hello, " + strings.TrimSpace(name) + " from native Go"
}

func NativeAdd(left int, right int) int {
	return left + right
}
