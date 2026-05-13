//go:build windows

package main

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func isAdmin() bool {
	f, err := os.Open(`\\.\PHYSICALDRIVE0`)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func relaunchElevated() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	exePath = resolveExecutablePathForElevation(exePath)

	args := strings.Join(os.Args[1:], " ")

	shell32 := windows.NewLazySystemDLL("shell32.dll")
	shellExecuteW := shell32.NewProc("ShellExecuteW")
	if err := shell32.Load(); err != nil {
		return false
	}
	if err := shellExecuteW.Find(); err != nil {
		return false
	}

	verbPtr, _ := windows.UTF16PtrFromString("runas")
	exePtr, _ := windows.UTF16PtrFromString(exePath)
	argsPtr, _ := windows.UTF16PtrFromString(args)

	ret, _, _ := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(argsPtr)),
		0,
		1,
	)

	// Per ShellExecute docs, return value > 32 means success.
	return ret > 32
}

func resolveExecutablePathForElevation(path string) string {
	if len(path) < 2 || path[1] != ':' {
		return path
	}

	drive := strings.ToUpper(path[:2])
	mpr := windows.NewLazySystemDLL("mpr.dll")
	wnetGetConnectionW := mpr.NewProc("WNetGetConnectionW")
	if err := mpr.Load(); err != nil {
		return path
	}
	if err := wnetGetConnectionW.Find(); err != nil {
		return path
	}

	localName, _ := windows.UTF16PtrFromString(drive)
	buf := make([]uint16, 2048)
	bufLen := uint32(len(buf))

	ret, _, _ := wnetGetConnectionW.Call(
		uintptr(unsafe.Pointer(localName)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)),
	)

	const noError = 0
	if ret != noError {
		return path
	}

	uncRoot := windows.UTF16ToString(buf)
	if uncRoot == "" {
		return path
	}

	// Keep the original subpath after the drive prefix (e.g. "\dist\app.exe").
	return uncRoot + path[2:]
}

func showElevationRequiredMessage() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	messageBoxW := user32.NewProc("MessageBoxW")
	if err := user32.Load(); err != nil {
		return
	}
	if err := messageBoxW.Find(); err != nil {
		return
	}

	textPtr, _ := windows.UTF16PtrFromString("Administrator privileges are required to create network adapters.\nPlease run LinkThings Client as Administrator.")
	titlePtr, _ := windows.UTF16PtrFromString("LinkThings Client")

	const mbIconError = 0x00000010
	const mbOk = 0x00000000

	_, _, _ = messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(textPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbOk|mbIconError),
	)
}
