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

// sessionEnvVars are forwarded explicitly to the elevated process via
// `pkexec env VAR=value ...` because pkexec scrubs almost the entire
// environment by default - without them a GUI child fails with GTK's
// "Failed to open display" even though the process is root and otherwise
// runs fine (root can read the invoking user's .Xauthority/Wayland socket
// once it knows where to look; pkexec just doesn't tell it by default).
// DBUS_SESSION_BUS_ADDRESS belongs here for the same reason: root has no
// D-Bus session of its own (no /run/user/0/bus), so without this the system
// tray's dbus.SessionBus() call fails silently (logged, not fatal) and the
// tray icon never registers, even though the window renders fine.
var sessionEnvVars = []string{"DISPLAY", "XAUTHORITY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"}

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

	argv := []string{"pkexec", "env"}
	for _, name := range sessionEnvVars {
		if v, ok := os.LookupEnv(name); ok {
			argv = append(argv, name+"="+v)
		}
	}
	argv = append(argv, exePath)
	argv = append(argv, os.Args[1:]...)

	if err := syscall.Exec(pkexecPath, argv, os.Environ()); err != nil {
		return false
	}
	// syscall.Exec only returns on error; a successful call never reaches here.
	return true
}

func (linuxElevator) ShowElevationRequiredMessage() {
	// Terminal TUI - the stderr message main.go already prints is sufficient.
}
