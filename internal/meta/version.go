// Package meta implements trimdown's own commands (not tool proxies):
// version, passthrough, savings, config.
package meta

import "fmt"

// Version is overridden at build time via -ldflags "-X .../meta.Version=v1.2.3".
var Version = "dev"

// ShowVersion prints the binary version.
func ShowVersion() int {
	fmt.Println("trimdown", Version)
	return 0
}
