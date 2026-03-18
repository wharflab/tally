//go:build cgo

package version

/*
#if defined(__GNUC__)
const char* tally_cc_version = __VERSION__;
#else
const char* tally_cc_version = "unknown";
#endif

#if defined(__GLIBC__)
#include <gnu/libc-version.h>
const char* tally_glibc_version(void) { return gnu_get_libc_version(); }
#else
const char* tally_glibc_version(void) { return ""; }
#endif
*/
import "C"

func cgoInfo() CGOInfo {
	info := CGOInfo{
		Enabled:   true,
		CCompiler: C.GoString(C.tally_cc_version),
	}
	if v := C.GoString(C.tally_glibc_version()); v != "" {
		info.GlibcVersion = v
	}
	return info
}
