package license

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const licenseURL = "https://gist.githubusercontent.com/yanmad27/351b482bf554ee3c6ba818ed14a9a0d3/raw"

// GetMacAddresses returns MACs of interfaces that are UP and have at least one IP address.
// This filters out WAN Miniport and other fake adapters.
func GetMacAddresses() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var macs []string

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		mac := strings.ToLower(iface.HardwareAddr.String())
		if mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		if !seen[mac] {
			seen[mac] = true
			macs = append(macs, mac)
		}
	}
	return macs
}

// CheckLicense fetches the allowed MAC list from the gist and returns true if this machine is licensed.
func CheckLicense() (bool, error) {
	url := fmt.Sprintf("%s?t=%d", licenseURL, time.Now().UnixMilli())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	allowedMacs := make(map[string]bool)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allowedMacs[line] = true
	}

	for _, mac := range GetMacAddresses() {
		if allowedMacs[mac] {
			return true, nil
		}
	}
	return false, nil
}
