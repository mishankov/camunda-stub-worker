//go:build bindings

package main

// Binding generation only needs the exported App method set. Avoid touching the user's database.
func buildApp() (*App, error) { return &App{}, nil }
