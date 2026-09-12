package main

// Elevator is every OS-specific privilege-check/relaunch primitive main.go
// needs. Adding a new OS means implementing this interface (plus a
// currentElevator() constructor) in one new build-tagged file.
type Elevator interface {
	IsAdmin() bool
	RelaunchElevated() bool
	ShowElevationRequiredMessage()
}
