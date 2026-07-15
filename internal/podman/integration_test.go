package podman

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestRealPodmanSecretLifecycle exercises every wrapper call against an
// actual podman: the JSON/template shapes here are real, not fixtures. It
// skips when no working podman is reachable (rootless podman inside a
// container often cannot set up user namespaces); set CREI_TEST_PODMAN to
// prefix the invocation, e.g. CREI_TEST_PODMAN="sudo -n podman".
func TestRealPodmanSecretLifecycle(t *testing.T) {
	bin := strings.Fields(os.Getenv("CREI_TEST_PODMAN"))
	if len(bin) == 0 {
		bin = []string{"podman"}
	}
	real := func(stdin []byte, args ...string) ([]byte, error) {
		full := append(append([]string{}, bin[1:]...), args...)
		cmd := exec.Command(bin[0], full...)
		if stdin != nil {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, &execError{args: args, msg: strings.TrimSpace(stderr.String())}
		}
		return stdout.Bytes(), nil
	}
	if _, err := real(nil, "secret", "ls"); err != nil {
		t.Skipf("no working podman (%v); set CREI_TEST_PODMAN to enable", err)
	}

	origRun, origRunIn := run, runIn
	t.Cleanup(func() { run, runIn = origRun, origRunIn })
	run = func(args ...string) ([]byte, error) { return real(nil, args...) }
	runIn = func(stdin []byte, args ...string) error { _, err := real(stdin, args...); return err }

	const managedName, foreignName = "crei-it-managed", "crei-it-foreign"
	value := []byte("v1-trailing\n\n") // trailing newlines must survive round-trips
	cleanup := func() {
		_ = RemoveSecret(managedName)
		_ = RemoveSecret(foreignName)
	}
	cleanup()
	t.Cleanup(cleanup)

	if err := CreateSecret(managedName, value, false); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := real(value, "secret", "create", "--", foreignName, "-"); err != nil {
		t.Fatalf("create foreign: %v", err)
	}

	names, err := ListSecrets()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !names[managedName] || !names[foreignName] {
		t.Fatalf("ListSecrets missing test secrets: %v", names)
	}

	infos, err := SecretInfos()
	if err != nil {
		t.Fatalf("secret infos: %v", err)
	}
	if !infos[managedName].Managed {
		t.Fatalf("labeled secret not reported managed: %+v", infos[managedName])
	}
	if infos[foreignName].Managed {
		t.Fatalf("unlabeled secret reported managed: %+v", infos[foreignName])
	}
	if infos[managedName].CreatedAt.IsZero() || infos[managedName].UpdatedAt.IsZero() {
		t.Fatalf("timestamps not parsed: %+v", infos[managedName])
	}

	got, err := ReadSecret(managedName)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(value) {
		t.Fatalf("value not byte-exact: %q != %q", got, value)
	}

	// Adopt path: re-create with the same value, then verify nothing changed.
	if err := CreateSecret(managedName, got, true); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if again, _ := ReadSecret(managedName); string(again) != string(value) {
		t.Fatalf("value drifted across replace: %q", again)
	}

	if err := RemoveSecret(managedName); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if names, _ := ListSecrets(); names[managedName] {
		t.Fatal("secret still present after remove")
	}
}

type execError struct {
	args []string
	msg  string
}

func (e *execError) Error() string {
	return "podman " + strings.Join(e.args, " ") + ": " + e.msg
}
