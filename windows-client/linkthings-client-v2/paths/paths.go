// Package paths resolves the OS-appropriate directories for config and log
// storage. It has no build tags: the branching lives in runtime.GOOS checks
// here, not in separate platform files, since this is just directory-string
// logic rather than an OS primitive that needs a Platform-style interface.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// ConfigDir returns the directory servers.json is stored in, creating it if
// necessary: %APPDATA%\LinkThings on Windows (unchanged from the original
// client), $XDG_CONFIG_HOME/linkthings or ~/.config/linkthings on Linux.
func ConfigDir() (string, error) {
	var dir string
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(homeDir, "AppData", "Roaming")
		}
		dir = filepath.Join(base, "LinkThings")
	} else {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "linkthings")
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// StateDir returns the directory client.log is stored in, creating it if
// necessary. On Windows this deliberately reuses ConfigDir() so logs stay at
// %APPDATA%\LinkThings\logs, matching the original client's behavior exactly
// rather than moving to %LocalAppData% as a side effect of adding Linux
// support. On Linux it's $XDG_CACHE_HOME/linkthings or ~/.cache/linkthings.
func StateDir() (string, error) {
	if runtime.GOOS == "windows" {
		return ConfigDir()
	}

	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "linkthings")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}
