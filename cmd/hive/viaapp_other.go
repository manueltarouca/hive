//go:build !darwin

package main

// startViaApp: only macOS has an app to start the server.
func startViaApp(string) bool { return false }
