package podman

import (
	"reflect"
	"testing"
)

func TestListSecretsParsesNames(t *testing.T) {
	orig := run
	defer func() { run = orig }()
	// Stub `podman secret ls --format {{.Name}}`: names one per line, with a
	// stray blank line that must be ignored.
	run = func(args ...string) ([]byte, error) {
		return []byte("alpha\nbeta\n\ngamma\n"), nil
	}

	got, err := ListSecrets()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"alpha": true, "beta": true, "gamma": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListSecrets() = %v, want %v", got, want)
	}
}

func TestListSecretsEmpty(t *testing.T) {
	orig := run
	defer func() { run = orig }()
	run = func(args ...string) ([]byte, error) { return []byte(""), nil }

	got, err := ListSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("ListSecrets() = %v, want empty", got)
	}
}
