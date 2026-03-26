//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("supcua-v2 requires Windows 10. This binary was built for a non-Windows platform.")
}
