//go:build linux

package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

type linuxElevator struct{}

func currentElevator() Elevator { return linuxElevator{} }

// IsAdmin reports whether this process already carries the one capability
// every privileged tunnel/adapter/routing operation in tunnel/tunnel_linux.go
// needs (CAP_NET_ADMIN) - not whether it's root. Mirrors the Capget pattern
// golang.org/x/sys/unix itself uses internally for the same kind of check
// (see isCapDacOverrideSet in its syscall_linux.go).
func (linuxElevator) IsAdmin() bool {
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return false
	}
	return data[0].Effective&(1<<unix.CAP_NET_ADMIN) != 0
}

// RelaunchElevated grants this binary the CAP_NET_ADMIN file capability
// (one pkexec prompt, authenticating a single short-lived `setcap` command,
// not the whole GUI) and then re-execs the same binary directly - no pkexec
// wrapper this time - so the fresh process picks up the capability from the
// file's metadata. A process can't gain a file capability retroactively;
// it's computed from the executable's extended attributes at exec() time,
// which is also why rebuilding the binary (a new inode) requires repeating
// this once.
//
// Deliberately NOT the old model of pkexec-relaunching the entire process as
// root: a root GUI process cannot join the invoking user's own D-Bus session
// bus (confirmed empirically - see CLAUDE.md's operational caveat), which
// broke the system tray and Wails' SingleInstance lock. Granting just the
// capability keeps this process's identity as the invoking user throughout,
// so that boundary never comes up.
func (linuxElevator) RelaunchElevated() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	pkexecPath, err := exec.LookPath("pkexec")
	if err != nil {
		return false
	}
	setcapPath, err := exec.LookPath("setcap")
	if err != nil {
		return false
	}

	if err := exec.Command(pkexecPath, setcapPath, "cap_net_admin+ep", exePath).Run(); err != nil {
		return false
	}

	if err := syscall.Exec(exePath, os.Args, os.Environ()); err != nil {
		return false
	}
	// syscall.Exec only returns on error; a successful call never reaches here.
	return true
}

func (linuxElevator) ShowElevationRequiredMessage() {
	// No native dialog on this path (unlike Windows) - the stderr message
	// main.go already prints before exiting is the only feedback the user
	// gets here, which is acceptable since this only triggers when pkexec
	// itself is missing or its one-line setcap grant failed/was cancelled.
}
