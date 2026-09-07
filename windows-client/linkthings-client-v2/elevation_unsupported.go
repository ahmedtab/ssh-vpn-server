//go:build !windows && !linux

package main

type unsupportedElevator struct{}

func currentElevator() Elevator { return unsupportedElevator{} }

func (unsupportedElevator) IsAdmin() bool                 { return true }
func (unsupportedElevator) RelaunchElevated() bool        { return false }
func (unsupportedElevator) ShowElevationRequiredMessage() {}
