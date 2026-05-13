//go:build !windows

package main

func isAdmin() bool {
	return true
}

func relaunchElevated() bool {
	return false
}

func showElevationRequiredMessage() {}
