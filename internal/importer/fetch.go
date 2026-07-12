package importer

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// maxRemoteSize caps a fetched compose file; anything larger is not a compose
// file.
const maxRemoteSize = 10 << 20

var httpClient = &http.Client{Timeout: 30 * time.Second}

// isRemote reports whether a path argument is an http(s) URL.
func isRemote(p string) bool {
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}

// resolveRemotePaths downloads any URL arguments (compose files and env
// files) into a temp directory and rewrites Options to point at the local
// copies. For remote compose files the temp subdirectory's name carries the
// URL-derived project name, so compose-go's working-dir fallback yields it
// while a name: field in the file (or --name) still wins; remote env files
// alone leave WorkingDir untouched. Returns a cleanup func for the temp tree.
func resolveRemotePaths(opts Options, warnf func(string, ...any)) (Options, map[string]string, func(), error) {
	labels := map[string]string{}
	remoteCompose := firstRemote(opts.Paths) != ""
	remoteEnv := firstRemote(opts.EnvFiles) != ""
	if !remoteCompose && !remoteEnv {
		return opts, labels, func() {}, nil
	}

	root, err := os.MkdirTemp("", "crei-import-*")
	if err != nil {
		return opts, labels, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }

	if remoteCompose {
		dirName := opts.ProjectName
		if dirName == "" {
			dirName = deriveProjectName(firstRemote(opts.Paths))
			if dirName == "" {
				cleanup()
				return opts, labels, func() {}, fmt.Errorf("cannot derive a project name from %s; pass --name", firstRemote(opts.Paths))
			}
		}
		dir := filepath.Join(root, dirName)
		if err := os.Mkdir(dir, 0o755); err != nil {
			cleanup()
			return opts, labels, func() {}, err
		}
		paths, err := fetchAll(opts.Paths, dir, "compose-%d.yaml", true, labels)
		if err != nil {
			cleanup()
			return opts, labels, func() {}, err
		}
		opts.Paths = paths
		opts.WorkingDir = dir
		warnf("fetched from a URL: relative paths in the emitted CUE (build contexts, bind mounts, env files) refer to the source repository layout")
	}
	if remoteEnv {
		envFiles, err := fetchAll(opts.EnvFiles, root, "import-%d.env", false, labels)
		if err != nil {
			cleanup()
			return opts, labels, func() {}, err
		}
		opts.EnvFiles = envFiles
	}
	return opts, labels, cleanup, nil
}

// fetchAll downloads the remote entries of a path list into dir, anchoring
// local entries absolute when anchorLocal is set (needed when WorkingDir
// moves to the temp dir).
func fetchAll(in []string, dir, fallbackPattern string, anchorLocal bool, labels map[string]string) ([]string, error) {
	out := make([]string, len(in))
	seen := map[string]bool{}
	for i, p := range in {
		if !isRemote(p) {
			out[i] = p
			if anchorLocal {
				abs, err := filepath.Abs(p)
				if err != nil {
					return nil, err
				}
				labels[abs] = p
				out[i] = abs
			}
			continue
		}
		base := path.Base(mustURLPath(p))
		if base == "" || base == "/" || base == "." || seen[base] {
			base = fmt.Sprintf(fallbackPattern, i)
		}
		seen[base] = true
		local := filepath.Join(dir, base)
		if err := fetchTo(local, p); err != nil {
			return nil, err
		}
		labels[local] = p
		out[i] = local
	}
	return out, nil
}

func firstRemote(paths []string) string {
	for _, p := range paths {
		if isRemote(p) {
			return p
		}
	}
	return ""
}

func mustURLPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Path
}

// rewriteRawURL turns browser file URLs into raw-content URLs: a GitHub
// /blob/ page or a GitLab /-/blob/ page serves HTML, not YAML.
func rewriteRawURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch {
	case u.Host == "github.com":
		// /OWNER/REPO/blob/REF/PATH -> raw.githubusercontent.com/OWNER/REPO/REF/PATH
		segs := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(segs) >= 5 && segs[2] == "blob" {
			u.Host = "raw.githubusercontent.com"
			u.Path = "/" + strings.Join(append(segs[:2], segs[3:]...), "/")
			return u.String()
		}
	case strings.Contains(u.Path, "/-/blob/"):
		// GitLab: /GROUP/PROJECT/-/blob/REF/PATH -> /-/raw/REF/PATH
		u.Path = strings.Replace(u.Path, "/-/blob/", "/-/raw/", 1)
		return u.String()
	}
	return raw
}

var composeNameSafe = regexp.MustCompile(`[^a-z0-9_-]+`)

// deriveProjectName picks a compose project name from a URL: the directory
// containing the file (the repository name when the file sits at the repo
// root). Returns "" when nothing usable remains.
func deriveProjectName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) < 2 {
		return ""
	}
	dirs := segs[:len(segs)-1] // drop the filename
	// Strip forge routing segments so refs/branches don't become names:
	// github.com/OWNER/REPO/blob/REF/..., raw.githubusercontent.com/OWNER/
	// REPO/REF/..., gitlab /-/blob|raw/REF/....
	switch {
	case u.Host == "github.com" && len(dirs) >= 4 && (dirs[2] == "blob" || dirs[2] == "raw"):
		if rest := dirs[4:]; len(rest) > 0 {
			return normalizeProjectName(rest[len(rest)-1])
		}
		return normalizeProjectName(dirs[1]) // repo name
	case u.Host == "raw.githubusercontent.com" && len(dirs) >= 3:
		if rest := dirs[3:]; len(rest) > 0 {
			return normalizeProjectName(rest[len(rest)-1])
		}
		return normalizeProjectName(dirs[1])
	default:
		if i := indexOf(dirs, "-"); i >= 0 {
			// GitLab /-/blob|raw/REF/...: dirs[i+1] is blob|raw, dirs[i+2] the
			// ref; the in-repo path starts at i+3. Name from that path, else
			// the project name before /-/.
			if i+3 <= len(dirs) {
				if rest := dirs[i+3:]; len(rest) > 0 {
					return normalizeProjectName(rest[len(rest)-1])
				}
			}
			if i >= 1 {
				return normalizeProjectName(dirs[i-1])
			}
			return ""
		}
	}
	return normalizeProjectName(dirs[len(dirs)-1])
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}

// normalizeProjectName lowers a candidate into compose's project-name
// alphabet ([a-z0-9_-], starting alphanumeric).
func normalizeProjectName(s string) string {
	s = composeNameSafe.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-_")
	return s
}

// fetchTo downloads a (rewritten) URL to a local file.
func fetchTo(local, raw string) error {
	target := rewriteRawURL(raw)
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", raw, err)
	}
	req.Header.Set("User-Agent", "crei-import-compose")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", raw, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: HTTP %s", raw, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteSize+1))
	if err != nil {
		return fmt.Errorf("fetch %s: %w", raw, err)
	}
	if len(body) > maxRemoteSize {
		return fmt.Errorf("fetch %s: response exceeds %d bytes; not a compose file", raw, maxRemoteSize)
	}
	if looksLikeHTML(body) {
		return fmt.Errorf("fetch %s: got an HTML page, not YAML; use the raw file URL", raw)
	}
	return os.WriteFile(local, body, 0o644)
}

// looksLikeHTML catches forge web pages served where YAML was expected.
func looksLikeHTML(b []byte) bool {
	head := strings.ToLower(string(b[:min(512, len(b))]))
	head = strings.TrimSpace(head)
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}
