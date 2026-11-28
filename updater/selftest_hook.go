package updater

import (
	"fmt"
	"net/url"
	"strconv"
)

// SelftestBase points CheckUpdate at a loopback stub and returns the function that restores the
// previous base. Only the Windows self-test calls it (ui/selftest_update_check_windows.go, which
// runs solely under the explicit -selftest flag, in a throw-away data directory). It accepts only
// http://127.0.0.1:<port> (no userinfo, path, query or fragment; port 1-65535), so the real GitHub
// API cannot be replaced by it. No build tag: nothing in production code calls it.
func SelftestBase(base string) (restore func(), err error) {
	u, perr := url.Parse(base)
	if perr != nil {
		return nil, fmt.Errorf("selftest api base is not a URL: %q", base)
	}
	port, _ := strconv.Atoi(u.Port())
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "" ||
		u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || port < 1 || port > 65535 {
		return nil, fmt.Errorf("selftest api base must be http://127.0.0.1:<port>, got %q", base)
	}
	baseMu.Lock()
	prev, prevStub := apiBase, stubbed
	apiBase, stubbed = base, true
	baseMu.Unlock()
	return func() { baseMu.Lock(); apiBase, stubbed = prev, prevStub; baseMu.Unlock() }, nil
}

// BaseIsSelftestStub reports whether the API base is the loopback stub.
func BaseIsSelftestStub() bool {
	baseMu.Lock()
	defer baseMu.Unlock()
	return stubbed
}
