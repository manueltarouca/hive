//go:build !darwin

package main

func agentServes(string, []string, []string) bool { return false }
func startAgent(string) (bool, bool)              { return false, false }
func agentHint(bool) string                       { return "" }
func prepareAgent(string) error                   { return nil }
func agentServing()                               {}
