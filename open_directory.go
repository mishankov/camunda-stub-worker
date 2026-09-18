package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func openDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open data directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("open data directory: %q is not a directory", path)
	}

	name, args, err := directoryOpenCommand(runtime.GOOS, path)
	if err != nil {
		return err
	}
	output, err := exec.Command(name, args...).CombinedOutput()
	if err == nil {
		return nil
	}
	if message := strings.TrimSpace(string(output)); message != "" {
		return fmt.Errorf("open data directory: %w: %s", err, message)
	}
	return fmt.Errorf("open data directory: %w", err)
}

func directoryOpenCommand(goos, path string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "/usr/bin/open", []string{path}, nil
	case "windows":
		return "explorer.exe", []string{path}, nil
	case "linux", "freebsd", "netbsd", "openbsd":
		return "xdg-open", []string{path}, nil
	default:
		return "", nil, errors.New("open data directory: unsupported operating system " + goos)
	}
}
