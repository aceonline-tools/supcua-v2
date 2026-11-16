//go:build !windows

package main

import "fmt"

func main() { fmt.Println("spike is Windows-only; see SMOKE.md") }
