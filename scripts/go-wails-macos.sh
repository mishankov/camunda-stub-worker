#!/bin/sh
# Match Go 1.27's minimum macOS version.
export CGO_CFLAGS="${CGO_CFLAGS:+$CGO_CFLAGS }-mmacosx-version-min=13.0"
# Wails 2.15 uses UniformTypeIdentifiers for file dialogs on modern macOS.
export CGO_LDFLAGS="${CGO_LDFLAGS:+$CGO_LDFLAGS }-mmacosx-version-min=13.0 -framework UniformTypeIdentifiers"
exec go "$@"
