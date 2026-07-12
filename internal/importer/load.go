package importer

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/sirupsen/logrus"
)

func init() {
	// compose-go logs per-variable "not set, defaulting to blank" warnings
	// through logrus's global logger; the conversion report carries one
	// aggregated warning instead, so silence the stderr spam. crei uses no
	// logrus of its own.
	logrus.SetOutput(io.Discard)
}

// load parses the compose project. By default interpolation is disabled so
// ${VAR} tokens survive into the model (and are lifted into the env: struct);
// with ResolveEnv they are substituted from EnvFiles / the OS environment.
func loadCompose(opts Options) (*types.Project, error) {
	wd := opts.WorkingDir
	if wd == "" {
		var err error
		if wd, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	fns := []composecli.ProjectOptionsFn{
		composecli.WithWorkingDirectory(wd),
		// Keep volume/env-file paths as written in the compose file rather
		// than resolving to absolute host paths.
		composecli.WithResolvedPaths(false),
		// Normalization fills in the implicit default network so service DNS
		// semantics carry over.
		composecli.WithNormalization(true),
		composecli.WithConsistency(true),
	}
	if opts.ProjectName != "" {
		fns = append(fns, composecli.WithName(opts.ProjectName))
	}
	if len(opts.Paths) == 0 {
		fns = append(fns, composecli.WithDefaultConfigPath)
	}
	// Never read/merge service-level env_file contents into environment:
	// the emitted CUE keeps them as EnvironmentFile= references (and for a
	// remote import the file isn't present locally at all). This is
	// independent of ${VAR} interpolation, whose values come from
	// WithEnvFiles/WithDotEnv/WithOsEnv below.
	fns = append(fns, composecli.WithoutEnvironmentResolution)
	if opts.ResolveEnv {
		for _, f := range opts.EnvFiles {
			fns = append(fns, composecli.WithEnvFiles(f))
		}
		fns = append(fns, composecli.WithDotEnv)
		if opts.UseOsEnv {
			fns = append(fns, composecli.WithOsEnv)
		}
	} else {
		fns = append(fns, composecli.WithInterpolation(false))
	}
	po, err := composecli.NewProjectOptions(opts.Paths, fns...)
	if err != nil {
		return nil, err
	}
	// A dotenv file passed positionally would misparse as a compose override
	// with a cryptic yaml error; catch it before compose-go does.
	for _, p := range opts.Paths {
		if !isRemote(p) && looksLikeEnvFile(p) {
			return nil, fmt.Errorf("%s looks like an env file (KEY=VALUE lines), not compose YAML; pass it with --env-file instead", p)
		}
	}
	project, err := po.LoadProject(context.Background())
	if err != nil {
		// A ${VAR} inside a structured field (ports, deploy limits, ...)
		// cannot survive un-interpolated: the typed decode fails with errors
		// that never mention variables ("expected a map or struct, got
		// string"). Detect the situation from the raw files instead of the
		// error text, and point at the resolve modes.
		if !opts.ResolveEnv && rawFilesUseVariables(po.ConfigPaths) {
			return nil, fmt.Errorf("%w\n\nthis compose file uses ${VAR} interpolation in structured fields (like ports), which cannot be preserved symbolically; resolve values at import time with one of:\n  --resolve            bake the file's own ${VAR:-default} values (plus .env if present)\n  --env-file <file>    bake values from an env file\n  --env                bake values from the current environment", err)
		}
		return nil, err
	}
	return project, nil
}

// looksLikeEnvFile sniffs a file for dotenv shape: every non-blank,
// non-comment line (of the first few) is KEY=VALUE with no YAML structure.
func looksLikeEnvFile(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	envLine := regexp.MustCompile(`^\s*(export\s+)?[A-Za-z_][A-Za-z0-9_.]*=`)
	checked := 0
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !envLine.MatchString(line) {
			return false
		}
		if checked++; checked >= 20 {
			break
		}
	}
	return checked > 0
}

// rawFilesUseVariables reports whether any compose file contains an
// interpolation token.
func rawFilesUseVariables(paths []string) bool {
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, tok := range varToken.FindAllString(string(raw), -1) {
			if tok != "$$" {
				return true
			}
		}
	}
	return false
}
