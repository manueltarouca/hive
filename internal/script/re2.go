package script

import (
	"fmt"
	"regexp"
	"sync"
)

// Patterns in rules and triggers are RE2 (Go's regexp): linear time, so a
// pattern can't hang the server on text a program printed.
var re2Cache sync.Map

func compileRE2(pattern string) (*regexp.Regexp, error) {
	if re, ok := re2Cache.Load(pattern); ok {
		return re.(*regexp.Regexp), nil
	}
	if len(pattern) > 1000 {
		return nil, fmt.Errorf("pattern longer than 1000 characters")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %v (patterns are RE2: no look-behind or back-references)", pattern, err)
	}
	re2Cache.Store(pattern, re)
	return re, nil
}
