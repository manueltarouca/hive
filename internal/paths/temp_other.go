//go:build !darwin

package paths

func userTempDir() string { return "" }
