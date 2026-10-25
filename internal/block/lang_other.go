//go:build !darwin

package block

// defaultLang: elsewhere the server has the login's locale, and en_US.UTF-8
// may not exist.
func defaultLang() string { return "" }
