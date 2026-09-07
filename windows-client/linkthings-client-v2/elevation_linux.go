//go:build linux

package main

import (
	"os"
	"os/exec"
	"syscall"
)

type linuxElevator struct{}

func currentElevator() Elevator { return linuxElevator{} }

func (linuxElevator) IsAdmin() bool {
	return os.Geteuid() == 0
}

// RelaunchElevated re-execs the current process under pkexec, which shows a
// graphical polkit prompt - the closest Linux equivalent to Windows's UAC
// self-relaunch. It replaces the current process image (rather than
// spawning a detached child) so the elevated process inherits this one's
// stdio/tty directly, matching how a TUI app is expected to keep running in
// the same terminal after relaunch.
func (linuxElevator) RelaunchElevated() bool {
	pkexecPath, err := exec.LookPath("pkexec")
	if err != nil {
		return false
	}
	exePath, err := os.Executable()
	if err != nil {
		return false
	}

	argv := append([]string{"pkexec", exePath}, os.Args[1:]...)
	if err := syscall.Exec(pkexecPath, argv, os.Environ()); err != nil {
		return false
	}
	// syscall.Exec only returns on error; a successful call never reaches here.
	return true
}

func (linuxElevator) ShowElevationRequiredMessage() {
	// Terminal TUI - the stderr message main.go already prints is sufficient.
}
