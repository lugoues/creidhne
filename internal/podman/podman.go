// Package podman wraps the podman CLI for the secret operations crei needs. It
// shells out to the podman binary (crei never links libpod), matching how the
// reconcile package shells out to systemctl. podman must be on PATH.
package podman

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// run executes `podman <args...>` and returns stdout. It is a package var so
// tests can stub the podman invocation.
var run = func(args ...string) ([]byte, error) {
	cmd := exec.Command("podman", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("podman not found on PATH; install podman to manage secrets")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("podman %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// ListSecrets returns the set of existing podman secret names. It uses a Go
// template format (not JSON) so it depends only on the stable `.Name` field, not
// the JSON shape, which has moved across podman versions.
func ListSecrets() (map[string]bool, error) {
	out, err := run("secret", "ls", "--format", "{{.Name}}")
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			names[s] = true
		}
	}
	return names, nil
}

// CreateSecret creates a podman secret from value, which is piped via stdin so
// it never appears in argv, the process table, or a temp file. With replace, an
// existing secret of the same name is overwritten.
func CreateSecret(name string, value []byte, replace bool) error {
	args := []string{"secret", "create"}
	if replace {
		args = append(args, "--replace")
	}
	args = append(args, name, "-")
	cmd := exec.Command("podman", args...)
	cmd.Stdin = bytes.NewReader(value)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("podman not found on PATH; install podman to manage secrets")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("podman secret create %s: %s", name, msg)
	}
	return nil
}
