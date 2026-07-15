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

// TestSecretInfosViaInspect: labels are only reachable through
// inspect (`ls --filter` has no label key; ls rows carry no Labels field and
// `--format json` renders the literal string "json"). The stub replays real
// podman 5.4 output shapes for both calls.
func TestSecretInfosViaInspect(t *testing.T) {
	orig := run
	defer func() { run = orig }()
	run = func(args ...string) ([]byte, error) {
		switch args[1] {
		case "ls":
			for _, a := range args {
				if a == "--filter" || a == "json" {
					t.Fatalf("ls must not use --filter or a json format: %v", args)
				}
			}
			return []byte("managed-one\nother-tool\n"), nil
		case "inspect":
			return []byte(`[
				{"ID": "b53", "Spec": {"Name": "managed-one", "Driver": {"Name": "file"}, "Labels": {"creidhne.managed": "true"}}},
				{"ID": "b08", "Spec": {"Name": "other-tool", "Driver": {"Name": "file"}, "Labels": {"manager": "compose"}}}
			]`), nil
		default:
			t.Fatalf("unexpected podman call: %v", args)
			return nil, nil
		}
	}

	got, err := SecretInfos()
	if err != nil {
		t.Fatal(err)
	}
	if !got["managed-one"].Managed || got["other-tool"].Managed {
		t.Errorf("SecretInfos() managed flags wrong: %+v", got)
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

// TestCreateSecretArgsSeparatesFlags is a security regression guard: a secret
// name beginning with "-" (e.g. "--replace" from a registry entry or CLI arg)
// must not be parsed by podman as a flag. A "--" separator must precede the
// name, placing it at the NAME positional (before the "-" stdin marker).
func TestCreateSecretArgsSeparatesFlags(t *testing.T) {
	args := createSecretArgs("--replace", false)

	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep == -1 {
		t.Fatalf("no %q separator before the name: %v", "--", args)
	}
	// Every created secret carries the ownership label prune scopes to.
	labeled := false
	for i := 0; i < sep; i++ {
		if args[i] == "--label" && i+1 < sep && args[i+1] == ManagedLabel {
			labeled = true
		}
	}
	if !labeled {
		t.Fatalf("create args missing --label %s: %v", ManagedLabel, args)
	}
	// The hostile name must be a positional after the separator, not a flag.
	if args[len(args)-2] != "--replace" || args[len(args)-1] != "-" {
		t.Fatalf("name not at NAME positional: %v", args)
	}
	// With replace=false, no real --replace flag may appear before the separator.
	for _, a := range args[:sep] {
		if a == "--replace" {
			t.Fatalf("name leaked into flags as --replace: %v", args)
		}
	}
}
