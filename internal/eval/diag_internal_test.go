package eval

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/lugoues/creidhne"
)

// TestFieldFromSchemaLine: the dispatch guard line of volumeStrings in the
// embedded schema resolves to its source field. The line number is found
// dynamically so schema edits cannot silently break the inference.
func TestFieldFromSchemaLine(t *testing.T) {
	raw, err := fs.ReadFile(creidhne.SchemaFS, "creidhne/container.cue")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	guard := 0
	for i, l := range lines {
		if strings.Contains(l, "for e in Container.Volume ") {
			guard = i + 1
			break
		}
	}
	if guard == 0 {
		t.Fatal("volumeStrings guard line not found in embedded schema")
	}
	if got := fieldFromSchemaLine("container.cue:" + strconv.Itoa(guard) + ":4"); got != "Container.Volume" {
		t.Fatalf("fieldFromSchemaLine = %q, want Container.Volume", got)
	}
	// Positions inside a dispatch body resolve via the upward scan to the
	// comprehension guard (labelStrings' _#renderLabel line -> Container.Label).
	inner := 0
	for i, l := range lines {
		if strings.Contains(l, "_#renderLabel & {#e: l}") {
			inner = i + 1
			break
		}
	}
	if inner == 0 {
		t.Fatal("labelStrings dispatch line not found in embedded schema")
	}
	if got := fieldFromSchemaLine("container.cue:" + strconv.Itoa(inner) + ":50"); got != "Container.Label" {
		t.Fatalf("upward scan = %q, want Container.Label", got)
	}
	if got := fieldFromSchemaLine("container.cue:1:1"); got != "" {
		t.Fatalf("package line should resolve to no field, got %q", got)
	}
}
