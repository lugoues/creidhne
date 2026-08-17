package eval

import (
	"strings"
	"testing"
)

// annotationOf returns the build-hash stamped on a unit's section, or "".
func annotationOf(u UnitRecord, section string) string {
	sec, ok := u.Data[section].(map[string]any)
	if !ok {
		return ""
	}
	list, _ := sec["Annotation"].([]any)
	for _, a := range list {
		if s, ok := a.(string); ok {
			if v, found := strings.CutPrefix(s, buildHashAnnotation+"="); found {
				return v
			}
		}
	}
	return ""
}

// buildUnit constructs a build UnitRecord with an inline Containerfile and one
// ImageTag.
func buildUnit(stem, containerfile, tag string) UnitRecord {
	return UnitRecord{
		Kind:     "build",
		Stem:     stem,
		Filename: stem + ".build",
		Data: map[string]any{
			"ContainerFile": containerfile,
			"Build":         map[string]any{"ImageTag": []any{tag}},
		},
	}
}

func containerUnit(stem, image string) UnitRecord {
	return UnitRecord{
		Kind:     "container",
		Stem:     stem,
		Filename: stem + ".container",
		Data: map[string]any{
			"imageString": image,
			"Container":   map[string]any{},
		},
	}
}

// TestInjectBuildHashesReferenceForms: a container gets the build's hash whether
// it references the build by its .build unit (Image=<stem>.build) or by the
// image tag (Image=<tag>). The tag form is the only one that works
// cross-quadlet, and was previously left unstamped.
func TestInjectBuildHashesReferenceForms(t *testing.T) {
	build := buildUnit("app-img", "FROM alpine\n", "localhost/app:latest")
	byUnit := containerUnit("by-unit", "app-img.build")
	byTag := containerUnit("by-tag", "localhost/app:latest")
	unrelated := containerUnit("other", "docker.io/nginx:latest")

	if err := injectBuildHashes([]Quadlet{{Name: "app", Units: []UnitRecord{build, byUnit, byTag, unrelated}}}); err != nil {
		t.Fatal(err)
	}

	want := annotationOf(build, "Build")
	if want == "" {
		t.Fatal("build unit did not get a hash")
	}
	if got := annotationOf(byUnit, "Container"); got != want {
		t.Fatalf("Image=<stem>.build consumer: got %q, want %q", got, want)
	}
	if got := annotationOf(byTag, "Container"); got != want {
		t.Fatalf("Image=<tag> consumer: got %q, want %q (the fixed disconnect)", got, want)
	}
	if got := annotationOf(unrelated, "Container"); got != "" {
		t.Fatalf("a container on an unrelated image must not be stamped, got %q", got)
	}
}

// TestInjectBuildHashesContextMoves: changing the build's inputs moves the hash
// on the build and on a tag-referencing consumer, so a context edit flags both.
func TestInjectBuildHashesContextMoves(t *testing.T) {
	hashFor := func(containerfile string) (buildH, consumerH string) {
		build := buildUnit("app-img", containerfile, "localhost/app:latest")
		consumer := containerUnit("app", "localhost/app:latest")
		if err := injectBuildHashes([]Quadlet{{Name: "app", Units: []UnitRecord{build, consumer}}}); err != nil {
			t.Fatal(err)
		}
		return annotationOf(build, "Build"), annotationOf(consumer, "Container")
	}

	b1, c1 := hashFor("FROM alpine\n")
	b2, c2 := hashFor("FROM alpine\nRUN apk add curl\n")
	if b1 == b2 {
		t.Fatal("a Containerfile change must move the build hash")
	}
	if c1 != b1 || c2 != b2 {
		t.Fatalf("consumer must track the build hash: %q/%q then %q/%q", c1, b1, c2, b2)
	}
	if c1 == c2 {
		t.Fatal("the consumer's hash must move with the build (the flag)")
	}
}

// TestInjectBuildHashesCrossQuadlet: a build in one quadlet flags a
// tag-referencing container in another; injection runs over the whole set.
func TestInjectBuildHashesCrossQuadlet(t *testing.T) {
	build := buildUnit("img", "FROM alpine\n", "localhost/shared:latest")
	consumer := containerUnit("app", "localhost/shared:latest")

	if err := injectBuildHashes([]Quadlet{
		{Name: "images", Units: []UnitRecord{build}},
		{Name: "app", Units: []UnitRecord{consumer}},
	}); err != nil {
		t.Fatal(err)
	}

	if got, want := annotationOf(consumer, "Container"), annotationOf(build, "Build"); got == "" || got != want {
		t.Fatalf("cross-quadlet tag consumer not stamped: got %q, want %q", got, want)
	}
}

// TestInjectBuildHashesDuplicateTag: two builds publishing the same ImageTag is
// a hard error — consumers reference the tag alone, so hash and ordering would
// follow whichever build a map iteration happened to visit last.
func TestInjectBuildHashesDuplicateTag(t *testing.T) {
	b1 := buildUnit("one", "FROM alpine\n", "localhost/dup:latest")
	b2 := buildUnit("two", "FROM busybox\n", "localhost/dup:latest")
	err := injectBuildHashes([]Quadlet{{Name: "app", Units: []UnitRecord{b1, b2}}})
	if err == nil {
		t.Fatal("want an error for a tag produced by two builds")
	}
	for _, name := range []string{"one.build", "two.build", "localhost/dup:latest"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error should name %q, got: %v", name, err)
		}
	}
}

// TestInjectBuildHashesTagConsumerOrdering: a tag-matched consumer gains
// Requires=/After=<build service> — the pair quadlet wires for
// Image=<stem>.build refs itself but cannot know about for raw tags. After=
// alone orders a joint restart; Requires= makes a failed rebuild block the
// consumer instead of restarting it against the old image.
func TestInjectBuildHashesTagConsumerOrdering(t *testing.T) {
	build := buildUnit("img", "FROM alpine\n", "localhost/app:latest")
	build.Service = "img-build.service"
	consumer := containerUnit("app", "localhost/app:latest")
	consumer.Service = "app.service"
	pre := containerUnit("pre", "localhost/app:latest")
	pre.Service = "pre.service"
	pre.Data["Unit"] = map[string]any{"After": []any{"img-build.service"}, "Requires": []any{"img-build.service"}}
	byUnit := containerUnit("by-unit", "img.build")
	byUnit.Service = "by-unit.service"

	if err := injectBuildHashes([]Quadlet{{Name: "app", Units: []UnitRecord{build, consumer, pre, byUnit}}}); err != nil {
		t.Fatal(err)
	}

	deps := func(u UnitRecord, directive string) []any {
		unit, _ := u.Data["Unit"].(map[string]any)
		list, _ := unit[directive].([]any)
		return list
	}
	for _, directive := range []string{"After", "Requires"} {
		if got := deps(consumer, directive); len(got) != 1 || got[0] != "img-build.service" {
			t.Errorf("tag consumer %s = %v, want [img-build.service]", directive, got)
		}
		if got := deps(pre, directive); len(got) != 1 {
			t.Errorf("already-declared %s must not be duplicated, got %v", directive, got)
		}
		if got := deps(byUnit, directive); len(got) != 0 {
			t.Errorf("Image=<stem>.build consumer is wired by quadlet itself; %s = %v, want none", directive, got)
		}
	}
}

// TestHashDataBinaryContent: two context payloads that differ only in invalid
// UTF-8 bytes must hash differently. encoding/json replaces invalid bytes with
// U+FFFD, which would otherwise make the encodings — and the hashes — collide.
func TestHashDataBinaryContent(t *testing.T) {
	payload := func(b byte) map[string]any {
		return map[string]any{"Context": map[string]any{"blob": string([]byte{0xde, b})}}
	}
	if hashData(payload(0xff)) == hashData(payload(0xfe)) {
		t.Fatal("binary contents differing in invalid UTF-8 bytes must not collide")
	}
	text := map[string]any{"Context": map[string]any{"f": "hello"}}
	if hashData(text) != hashData(map[string]any{"Context": map[string]any{"f": "hello"}}) {
		t.Fatal("hashing must stay deterministic for text data")
	}
	// Literal text spelling a binary marker must not collide with the binary
	// content that produces that marker (domain separation via "!text:").
	binary := string([]byte{0xde, 0xff})
	impostor := hashableString(binary) // the marker binary normalizes to
	if hashData(map[string]any{"Context": map[string]any{"f": binary}}) ==
		hashData(map[string]any{"Context": map[string]any{"f": impostor}}) {
		t.Fatal("marker-impersonating text must not collide with the binary it names")
	}
}
