package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	app, err := buildApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Initialisation error:", err)
		return
	}
	err = wails.Run(&options.App{Title: "Camunda Stub Worker", Width: 1280, Height: 820, MinWidth: 1024, MinHeight: 700, BackgroundColour: &options.RGBA{R: 14, G: 18, B: 25, A: 1}, AssetServer: &assetserver.Options{Assets: assets}, OnStartup: app.startup, OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose, Bind: []interface{}{app}, Linux: &linux.Options{Icon: appIcon, ProgramName: "Camunda Stub Worker"}})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Startup error:", err)
	}
}
