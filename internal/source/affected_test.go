package source

import (
	"strings"
	"testing"
)

func TestAffectedRejectsMalformedFrontmatter(t *testing.T) {
	root := t.TempDir()
	b := testBundle(root)
	writeTestFile(t, root, "src/thing.go", "package thing\n")
	writeTestFile(t, root, "docs/broken.md", "---\ntype: Reference\nsources:\n  - resource: bad: value\n---\n# Broken\n")

	_, err := Affected(b, "src/thing.go")
	if err == nil || !strings.Contains(err.Error(), "corpus document /broken.md") || !strings.Contains(err.Error(), "invalid YAML") {
		t.Fatalf("Affected error = %v, want document-qualified YAML failure", err)
	}
}
