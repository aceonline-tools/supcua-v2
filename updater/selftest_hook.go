package updater

import (
	"fmt"
	"net/url"
)

// SetAPIBaseForSelftest points CheckUpdate at a loopback stub. It is called only by the Windows
// self-test (ui/selftest_*_windows.go, which runs solely under the explicit -selftest flags in a
// throw-away data directory) and refuses anything but an http loopback address, so the real
// GitHub API cannot be replaced by it.
func SetAPIBaseForSelftest(base string) error {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "" {
		return fmt.Errorf("selftest api base must be http://127.0.0.1:<port>, got %q", base)
	}
	apiBase = base
	return nil
}
