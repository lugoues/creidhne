// Package podman wraps the podman CLI for the secret operations crei needs. It
// shells out to the podman binary (crei never links libpod), matching how the
// reconcile package shells out to systemctl. podman must be on PATH.
package podman

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
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

// ManagedLabel marks secrets created by crei. Prune only ever considers
// secrets carrying it: everything else on the host belongs to someone else.
const (
	managedLabelKey   = "creidhne.managed"
	managedLabelValue = "true"
	ManagedLabel      = managedLabelKey + "=" + managedLabelValue
)

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

// SecretInfo is what crei knows about one existing podman secret.
type SecretInfo struct {
	Managed    bool // carries the crei ownership label
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Labels     map[string]string // all labels, including foreign ones
	Driver     string            // storage driver name ("file" is podman's default)
	DriverOpts map[string]string
}

// SecretInfos returns every existing secret with its label state and
// timestamps. Labels are reachable only through inspect: `secret ls
// --filter` supports only name/id, and the ls rows carry no Labels field in
// any format (a `--format json` even renders the literal string "json"; ls
// formats are templates only). So: list the names, then one batch inspect.
// A secret deleted between the two calls fails the inspect; rerunning
// resolves it.
func SecretInfos() (map[string]SecretInfo, error) {
	names, err := ListSecrets()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return map[string]SecretInfo{}, nil
	}
	args := []string{"secret", "inspect", "--"}
	for name := range names {
		args = append(args, name)
	}
	out, err := run(args...)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		CreatedAt time.Time `json:"CreatedAt"`
		UpdatedAt time.Time `json:"UpdatedAt"`
		Spec      struct {
			Name   string            `json:"Name"`
			Labels map[string]string `json:"Labels"`
			Driver struct {
				Name    string            `json:"Name"`
				Options map[string]string `json:"Options"`
			} `json:"Driver"`
		} `json:"Spec"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parse podman secret inspect: %w", err)
	}
	infos := map[string]SecretInfo{}
	for _, r := range rows {
		if r.Spec.Name == "" {
			continue
		}
		infos[r.Spec.Name] = SecretInfo{
			Managed:    r.Spec.Labels[managedLabelKey] == managedLabelValue,
			CreatedAt:  r.CreatedAt,
			UpdatedAt:  r.UpdatedAt,
			Labels:     r.Spec.Labels,
			Driver:     r.Spec.Driver.Name,
			DriverOpts: r.Spec.Driver.Options,
		}
	}
	return infos, nil
}

// RemoveSecret deletes a podman secret. podman itself refuses secrets in use
// by a container; the error is returned as-is for the caller to surface.
func RemoveSecret(name string) error {
	_, err := run("secret", "rm", "--", name)
	return err
}

// ReadSecret returns a secret's value via inspect --showsecret, using a Go
// template rather than JSON: JSON strings cannot carry invalid UTF-8, so a
// binary secret would come back with its invalid bytes replaced and adopt
// --replace would then destroy the original. The template prints the raw
// bytes plus exactly one trailing newline appended by podman's writer, which
// is stripped; the value's own newlines (trailing ones included) survive.
func ReadSecret(name string) ([]byte, error) {
	out, err := run("secret", "inspect", "--showsecret", "--format", "{{.SecretData}}", "--", name)
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out, []byte("\n")), nil
}

// createSecretArgs builds the argv for `podman secret create`. The "--"
// separator is load-bearing: without it a secret name that begins with "-"
// (e.g. "--replace") is parsed by podman as a flag rather than the NAME
// positional. With "--", such a name reaches podman as a positional and is
// rejected cleanly by podman's own name validation instead of injecting a flag.
func createSecretArgs(name string, replace bool) []string {
	args := []string{"secret", "create", "--label", ManagedLabel}
	if replace {
		args = append(args, "--replace")
	}
	return append(args, "--", name, "-")
}

// runIn is run with a stdin payload; a package var so tests (including the
// real-podman integration test) can redirect it alongside run.
var runIn = func(stdin []byte, args ...string) error {
	cmd := exec.Command("podman", args...)
	cmd.Stdin = bytes.NewReader(stdin)
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
		return fmt.Errorf("podman %s: %s", strings.Join(args, " "), msg)
	}
	return nil
}

// CreateSecret creates a podman secret from value, which is piped via stdin so
// it never appears in argv, the process table, or a temp file. With replace, an
// existing secret of the same name is overwritten.
func CreateSecret(name string, value []byte, replace bool) error {
	return runIn(value, createSecretArgs(name, replace)...)
}

// AdoptSecret re-creates an existing secret with the crei ownership label
// while preserving everything else about it: its other labels and its storage
// driver (name and options). A plain CreateSecret would silently reset a
// pass/shell-driver secret to the default file driver and drop another
// tool's labels — changing far more than ownership metadata.
func AdoptSecret(name string, value []byte, info SecretInfo) error {
	args := []string{"secret", "create", "--replace", "--label", ManagedLabel}
	for k, v := range info.Labels {
		if k == managedLabelKey {
			continue
		}
		args = append(args, "--label", k+"="+v)
	}
	if info.Driver != "" && info.Driver != "file" {
		args = append(args, "--driver", info.Driver)
	}
	for k, v := range info.DriverOpts {
		args = append(args, "--driver-opts", k+"="+v)
	}
	return runIn(value, append(args, "--", name, "-")...)
}
