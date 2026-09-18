package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDirectoryOpenCommand(t *testing.T) {
	path := "/directory with spaces/data"
	tests := []struct {
		goos string
		name string
		args []string
	}{
		{goos: "darwin", name: "/usr/bin/open", args: []string{path}},
		{goos: "windows", name: "explorer.exe", args: []string{path}},
		{goos: "linux", name: "xdg-open", args: []string{path}},
		{goos: "freebsd", name: "xdg-open", args: []string{path}},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			name, args, err := directoryOpenCommand(tt.goos, path)
			if err != nil {
				t.Fatal(err)
			}
			if name != tt.name {
				t.Fatalf("name = %q, want %q", name, tt.name)
			}
			if !reflect.DeepEqual(args, tt.args) {
				t.Fatalf("args = %#v, want %#v", args, tt.args)
			}
		})
	}
}

func TestDirectoryOpenCommandRejectsUnsupportedOS(t *testing.T) {
	if _, _, err := directoryOpenCommand("plan9", "/tmp"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestOpenDirectoryRejectsInvalidPaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := openDirectory(missing); err == nil {
		t.Fatal("expected an error for a missing directory")
	}

	file := filepath.Join(t.TempDir(), "data.db")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := openDirectory(file); err == nil {
		t.Fatal("expected an error for a file path")
	}
}
