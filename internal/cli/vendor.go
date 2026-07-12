package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// vendorLockName is the pin file, next to what it describes.
const vendorLockName = "crei-vendor.json"

// vendorLock records every vendored module: the ref the user asked for, the
// commit it resolved to, and a content hash of the vendored tree so drift is
// detectable offline.
type vendorLock struct {
	Modules map[string]vendorPin `json:"modules"`
}

type vendorPin struct {
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
	Hash   string `json:"hash"`
}

func newVendorCmd() *cobra.Command {
	var source string
	var check bool
	cmd := &cobra.Command{
		Use:   "vendor [module[@ref]]",
		Short: "Vendor a CUE helper module into cue.mod/usr for offline use",
		Long: "vendor fetches a git-hosted CUE module and copies its .cue files into\n" +
			"cue.mod/usr/<module-path>/, the same offline layout the embedded schema\n" +
			"uses, recording the resolved commit and a tree hash in\n" +
			"cue.mod/" + vendorLockName + ".\n\n" +
			"The module path doubles as the git URL (https://<module>.git); pass\n" +
			"--source for private remotes or local paths. A vendored module may only\n" +
			"import the CUE standard library, itself, and the creidhne schema:\n" +
			"transitive module dependencies are refused.\n\n" +
			"With no arguments, re-fetches every module in the lock at its recorded\n" +
			"ref. --check verifies the vendored trees against the lock offline and\n" +
			"exits non-zero on drift.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			cueMod := filepath.Join(cfg.ProjectDir, "cue.mod")
			if _, err := os.Stat(cueMod); err != nil {
				return fmt.Errorf("no cue.mod in %s (run crei init first)", cfg.ProjectDir)
			}
			lock, err := loadVendorLock(cueMod)
			if err != nil {
				return err
			}

			if check {
				if len(args) > 0 {
					return errors.New("--check takes no module argument")
				}
				return checkVendored(out, cueMod, lock)
			}

			if len(args) == 1 {
				module, ref := splitModuleRef(args[0])
				pin, err := vendorModule(out, cueMod, module, ref, source)
				if err != nil {
					return err
				}
				lock.Modules[module] = *pin
				return writeVendorLock(cueMod, lock)
			}

			// No args: restore every pinned module at its recorded ref.
			if len(lock.Modules) == 0 {
				return errors.New("nothing to vendor: pass module[@ref], or see crei vendor --help")
			}
			for _, module := range sortedKeys(lock.Modules) {
				pin := lock.Modules[module]
				updated, err := vendorModule(out, cueMod, module, pin.Ref, "")
				if err != nil {
					return err
				}
				if updated.Commit != pin.Commit {
					fmt.Fprintf(out, "note: %s@%s now resolves to %.12s (was %.12s)\n", module, pin.Ref, updated.Commit, pin.Commit)
				}
				lock.Modules[module] = *updated
			}
			return writeVendorLock(cueMod, lock)
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "git URL or local path to fetch from (default https://<module>.git)")
	cmd.Flags().BoolVar(&check, "check", false, "verify vendored trees against the lock (offline); exit non-zero on drift")
	return cmd
}

func splitModuleRef(arg string) (module, ref string) {
	if i := strings.LastIndex(arg, "@"); i > 0 {
		return arg[:i], arg[i+1:]
	}
	return arg, ""
}

// vendorModule fetches module at ref from source (or the module's own https
// URL), validates it, and installs it under cue.mod/usr. Returns the pin.
func vendorModule(out io.Writer, cueMod, module, ref, source string) (*vendorPin, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("crei vendor needs git on PATH")
	}
	if source == "" {
		source = "https://" + module + ".git"
	}
	tmp, err := os.MkdirTemp("", "crei-vendor-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	cloneArgs := []string{"clone", "--quiet", "--depth", "1"}
	if ref != "" {
		cloneArgs = append(cloneArgs, "--branch", ref)
	}
	cloneArgs = append(cloneArgs, source, tmp)
	if msg, err := runGit("", cloneArgs...); err != nil {
		return nil, fmt.Errorf("clone %s: %v\n%s", source, err, msg)
	}
	commit, err := runGit(tmp, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD: %v", err)
	}
	commit = strings.TrimSpace(commit)

	// The fetched repo must be the module it claims to be.
	declared, err := moduleDeclaration(tmp)
	if err != nil {
		return nil, err
	}
	if declared != module {
		return nil, fmt.Errorf("%s declares module %q, not %q", source, declared, module)
	}
	files, err := collectModuleFiles(tmp)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s contains no .cue files outside cue.mod", source)
	}
	if err := checkImports(module, files); err != nil {
		return nil, err
	}

	// Install: replace the module's tree under cue.mod/usr wholesale.
	dest := filepath.Join(cueMod, "usr", filepath.FromSlash(module))
	if err := os.RemoveAll(dest); err != nil {
		return nil, err
	}
	paths := sortedKeys(files)
	for _, rel := range paths {
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, files[rel], 0o644); err != nil {
			return nil, err
		}
	}
	refShown := ref
	if refShown == "" {
		refShown = "HEAD"
	}
	fmt.Fprintf(out, "vendored %s@%s (%.12s): %d file(s) into cue.mod/usr/%s\n", module, refShown, commit, len(files), module)
	return &vendorPin{Ref: ref, Commit: commit, Hash: hashTree(files)}, nil
}

// moduleDeclaration reads the module path (major-version suffix stripped)
// from the repo's cue.mod/module.cue.
func moduleDeclaration(repo string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(repo, "cue.mod", "module.cue"))
	if err != nil {
		return "", fmt.Errorf("not a CUE module (no cue.mod/module.cue): %w", err)
	}
	m := regexp.MustCompile(`module:\s*"([^"]+)"`).FindSubmatch(raw)
	if m == nil {
		return "", errors.New("cue.mod/module.cue has no module declaration")
	}
	declared := string(m[1])
	if i := strings.LastIndex(declared, "@"); i > 0 {
		declared = declared[:i]
	}
	return declared, nil
}

// collectModuleFiles gathers the module's .cue files (relative slash paths),
// excluding the cue.mod directory and hidden directories.
func collectModuleFiles(repo string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "cue.mod" || strings.HasPrefix(d.Name(), ".") && path != repo {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".cue") {
			return nil
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = raw
		return nil
	})
	return files, err
}

var importLine = regexp.MustCompile(`(?m)^\s*(?:[A-Za-z_][A-Za-z0-9_]*\s+)?"([^"]+)"`)

// checkImports enforces the no-transitive-dependencies rule: a vendored
// module may import the CUE standard library (paths without a domain), the
// creidhne schema, and itself.
func checkImports(module string, files map[string][]byte) error {
	var offending []string
	for _, rel := range sortedKeys(files) {
		for _, imp := range cueImports(files[rel]) {
			first, _, _ := strings.Cut(imp, "/")
			base, _, _ := strings.Cut(imp, "@")
			switch {
			case !strings.Contains(first, "."): // stdlib (list, strings, encoding/json, ...)
			case base == "github.com/lugoues/creidhne" || strings.HasPrefix(base, "github.com/lugoues/creidhne/"):
			case base == module || strings.HasPrefix(base, module+"/"):
			default:
				offending = append(offending, fmt.Sprintf("%s imports %q", rel, imp))
			}
		}
	}
	if len(offending) > 0 {
		return fmt.Errorf("transitive module dependencies are not supported:\n  %s", strings.Join(offending, "\n  "))
	}
	return nil
}

// cueImports extracts import paths from a CUE file: both the single-line form
// and the parenthesized block.
func cueImports(src []byte) []string {
	var out []string
	// Block form: import ( "a" x "b" )
	block := regexp.MustCompile(`(?s)import\s*\((.*?)\)`)
	for _, m := range block.FindAllSubmatch(src, -1) {
		for _, im := range importLine.FindAllSubmatch(m[1], -1) {
			out = append(out, string(im[1]))
		}
	}
	// Single form: import "a" / import alias "a"
	single := regexp.MustCompile(`(?m)^import\s+(?:[A-Za-z_][A-Za-z0-9_]*\s+)?"([^"]+)"`)
	for _, m := range single.FindAllSubmatch(src, -1) {
		out = append(out, string(m[1]))
	}
	return out
}

// hashTree is a stable content hash over the vendored files.
func hashTree(files map[string][]byte) string {
	h := sha256.New()
	for _, rel := range sortedKeys(files) {
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(files[rel])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hashVendoredDir recomputes the tree hash of an installed module.
func hashVendoredDir(dir string) (string, int, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".cue") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = raw
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return hashTree(files), len(files), nil
}

// checkVendored verifies every pinned module's tree offline.
func checkVendored(out io.Writer, cueMod string, lock *vendorLock) error {
	if len(lock.Modules) == 0 {
		fmt.Fprintln(out, "vendor lock is empty; nothing to check")
		return nil
	}
	dirty := false
	for _, module := range sortedKeys(lock.Modules) {
		pin := lock.Modules[module]
		dir := filepath.Join(cueMod, "usr", filepath.FromSlash(module))
		hash, n, err := hashVendoredDir(dir)
		switch {
		case err != nil || n == 0:
			fmt.Fprintf(out, "%s: missing (re-run crei vendor)\n", module)
			dirty = true
		case hash != pin.Hash:
			fmt.Fprintf(out, "%s: drifted from %s@%.12s (re-run crei vendor)\n", module, pin.Ref, pin.Commit)
			dirty = true
		default:
			fmt.Fprintf(out, "%s: ok (%s@%.12s)\n", module, pin.Ref, pin.Commit)
		}
	}
	if dirty {
		return errSilent{}
	}
	return nil
}

func loadVendorLock(cueMod string) (*vendorLock, error) {
	lock := &vendorLock{Modules: map[string]vendorPin{}}
	raw, err := os.ReadFile(filepath.Join(cueMod, vendorLockName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return lock, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, lock); err != nil {
		return nil, fmt.Errorf("parse cue.mod/%s: %w", vendorLockName, err)
	}
	if lock.Modules == nil {
		lock.Modules = map[string]vendorPin{}
	}
	return lock, nil
}

func writeVendorLock(cueMod string, lock *vendorLock) error {
	raw, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cueMod, vendorLockName), append(raw, '\n'), 0o644)
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}
