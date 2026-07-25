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

	injectBuildHashes([]Quadlet{{Name: "app", Units: []UnitRecord{build, byUnit, byTag, unrelated}}})

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
		injectBuildHashes([]Quadlet{{Name: "app", Units: []UnitRecord{build, consumer}}})
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

	injectBuildHashes([]Quadlet{
		{Name: "images", Units: []UnitRecord{build}},
		{Name: "app", Units: []UnitRecord{consumer}},
	})

	if got, want := annotationOf(consumer, "Container"), annotationOf(build, "Build"); got == "" || got != want {
		t.Fatalf("cross-quadlet tag consumer not stamped: got %q, want %q", got, want)
	}
}
