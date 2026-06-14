// Command crei renders Podman Quadlet systemd units from typed CUE
// definitions and reconciles them against a quadlet directory.
package main

import "github.com/lugoues/creidhne/internal/cli"

func main() {
	cli.Execute()
}
