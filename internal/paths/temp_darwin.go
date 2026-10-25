package paths

/*
#include <unistd.h>
*/
import "C"

import "unsafe"

// userTempDir is the per-user temporary directory macOS gives every login.
func userTempDir() string {
	var buf [1024]C.char
	if n := C.confstr(C._CS_DARWIN_USER_TEMP_DIR, &buf[0], C.size_t(len(buf))); n == 0 || int(n) > len(buf) {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
}
