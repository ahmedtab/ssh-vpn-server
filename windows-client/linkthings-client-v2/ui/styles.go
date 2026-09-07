package ui

// This file contains helper functions and utilities for the UI package

// ImportantStyle returns a style for important text
func ImportantStyle() func(string) string {
	return func(s string) string {
		return "\033[1;32m" + s + "\033[0m" // Bold green
	}
}

// WarningStyle returns a style for warning text
func WarningStyle() func(string) string {
	return func(s string) string {
		return "\033[1;33m" + s + "\033[0m" // Bold yellow
	}
}

// ErrorStyle returns a style for error text
func ErrorStyle() func(string) string {
	return func(s string) string {
		return "\033[1;31m" + s + "\033[0m" // Bold red
	}
}
