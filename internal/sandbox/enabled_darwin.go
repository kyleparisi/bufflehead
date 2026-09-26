//go:build darwin

package sandbox

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <Security/SecTask.h>
#include <CoreFoundation/CoreFoundation.h>
static int bh_spike_sandbox_enabled(void) {
 SecTaskRef task = SecTaskCreateFromSelf(kCFAllocatorDefault);
 if (!task) return 0;
 CFTypeRef value = SecTaskCopyValueForEntitlement(task, CFSTR("com.apple.security.app-sandbox"), NULL);
 CFRelease(task);
 int enabled = value && CFGetTypeID(value) == CFBooleanGetTypeID() && CFBooleanGetValue((CFBooleanRef)value);
 if (value) CFRelease(value);
 return enabled;
}
*/
import "C"

func sandboxEnabled() bool { return C.bh_spike_sandbox_enabled() != 0 }
