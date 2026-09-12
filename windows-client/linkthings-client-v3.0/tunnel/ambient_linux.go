//go:build linux

package tunnel

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
	"linkthings.io/client-v3/logging"
)

// withNetAdminAmbient runs fn with CAP_NET_ADMIN temporarily raised into this
// goroutine's OS thread's ambient set, so the ip/resolvectl subprocess fn
// starts (neither carries any file capability of its own) inherits it, then
// lowers it again before returning.
//
// This used to be raised once, process-wide, right after main.go's elevation
// gate resolved, for the app's entire lifetime. That leaked CAP_NET_ADMIN
// into every child process this app's WebKitGTK webview spawns internally,
// including the bwrap sandbox helper it uses for its Network/Web processes -
// bwrap refuses to run the moment it sees a non-setuid process holding a
// capability ("Unexpected capabilities but not setuid, old file caps
// config?"), which crashed every unprivileged launch. Scoping the raise to
// only the few milliseconds an actual ip/resolvectl call takes - never live
// while the window/webview exists - avoids that collision entirely.
//
// Ambient capabilities are per-OS-thread kernel state, not per-goroutine or
// per-process, and Go otherwise migrates a goroutine across OS threads
// freely at its own scheduling points. runtime.LockOSThread pins this
// goroutine to one thread for the duration so the raise, fn, and lower all
// land on the same thread; UnlockOSThread (rather than letting the thread
// die with the goroutine) matters here because the caller's goroutine keeps
// running non-privileged code afterward - an SSH dial, event emission, etc.
// - and must not do so on a thread that still carries the capability.
func withNetAdminAmbient(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := raiseNetAdminAmbient(); err != nil {
		return fmt.Errorf("raise cap_net_admin ambient: %w", err)
	}
	defer func() {
		if err := lowerNetAdminAmbient(); err != nil {
			logging.Errorf("lower_cap_net_admin_ambient_failed err=%v", err)
		}
	}()

	return fn()
}

// raiseNetAdminAmbient adds CAP_NET_ADMIN to this thread's own inheritable
// set (legal: a thread can always add to its inheritable set a capability
// already in its own permitted set, which the RelaunchElevated file
// capability guarantees) and then raises it into the ambient set - both
// preconditions PR_CAP_AMBIENT_RAISE requires.
func raiseNetAdminAmbient() error {
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return fmt.Errorf("capget failed: %w", err)
	}

	data[0].Inheritable |= 1 << unix.CAP_NET_ADMIN
	if err := unix.Capset(&hdr, &data[0]); err != nil {
		return fmt.Errorf("capset (add CAP_NET_ADMIN to inheritable) failed: %w", err)
	}

	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_RAISE, uintptr(unix.CAP_NET_ADMIN), 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_CAP_AMBIENT_RAISE, CAP_NET_ADMIN) failed: %w", err)
	}
	return nil
}

// lowerNetAdminAmbient removes CAP_NET_ADMIN from the ambient set only - the
// inheritable bit raiseNetAdminAmbient set is left alone, since inheritable
// alone (without ambient) grants nothing to a child exec'd from a
// non-file-capable, non-setuid binary like this one.
func lowerNetAdminAmbient() error {
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_LOWER, uintptr(unix.CAP_NET_ADMIN), 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_CAP_AMBIENT_LOWER, CAP_NET_ADMIN) failed: %w", err)
	}
	return nil
}
