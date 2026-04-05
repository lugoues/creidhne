// Package creidhne embeds the CUE schema module and the unit-file templates so
// the CLI is self-contained: it resolves `import "github.com/lugoues/creidhne@v0"`
// offline (via an overlay built from SchemaFS) and renders unit files without
// any external files on disk.
package creidhne

import "embed"

// SchemaFS holds the CUE schema module. Paths are rooted at "creidhne/", e.g.
// "creidhne/container.cue" and "creidhne/cue.mod/module.cue".
//
//go:embed creidhne
var SchemaFS embed.FS

// TemplatesFS holds the unit-file text/templates, rooted at "templates/".
//
//go:embed templates/*.tpl
var TemplatesFS embed.FS
