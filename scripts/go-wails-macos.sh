#!/bin/sh
# Wails 2.15 uses UniformTypeIdentifiers for file dialogs on modern macOS.
export CGO_LDFLAGS="${CGO_LDFLAGS:+$CGO_LDFLAGS }-framework UniformTypeIdentifiers"
exec go "$@"
