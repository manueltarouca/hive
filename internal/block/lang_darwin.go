package block

import (
	"os"
	"os/exec"
	"strings"
	"sync"
)

var defaultLang = sync.OnceValue(func() string {
	out, _ := exec.Command("/usr/bin/defaults", "read", "-g", "AppleLocale").Output()
	return langFor(strings.TrimSpace(string(out)), func(l string) bool {
		_, err := os.Stat("/usr/share/locale/" + l)
		return err == nil
	})
})

// langFor is the UTF-8 locale for a macOS locale such as en_PT or pt_PT@rg=ptzzzz,
// if the system has it, else en_US.UTF-8.
func langFor(appleLocale string, exists func(string) bool) string {
	l, _, _ := strings.Cut(appleLocale, "@")
	if l != "" && exists(l+".UTF-8") {
		return l + ".UTF-8"
	}
	return "en_US.UTF-8"
}
