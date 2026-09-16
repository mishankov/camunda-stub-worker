package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := buildApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Initialisation error:", err)
		return
	}
	err = wails.Run(&options.App{Title: "Camunda Stub Worker", Width: 1280, Height: 820, MinWidth: 1024, MinHeight: 700, BackgroundColour: &options.RGBA{R: 14, G: 18, B: 25, A: 1}, AssetServer: &assetserver.Options{Assets: assets}, OnStartup: app.startup, OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose, Bind: []interface{}{app}})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Startup error:", err)
	}
}
