package compiler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/internal/source"
)

// discoverProject builds a minimal, directly-authored corpus whose root index
// carries the given external links. Discovery scans every corpus document, so
// the links become candidates without any Renders or renderer dependency.
func discoverProject(t *testing.T, externalLinks ...string) (string, *bundle.Bundle) {
	t.Helper()
	root := t.TempDir()
	var body strings.Builder
	body.WriteString("---\nokf_version: \"0.2\"\n---\n# Direct\n")
	for _, link := range externalLinks {
		body.WriteString("\n[external](")
		body.WriteString(link)
		body.WriteString(")\n")
	}
	if err := os.WriteFile(filepath.Join(root, "index.md"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, &bundle.Bundle{Root: root, Dir: root, Name: "Direct Corpus", Output: "dist", Spec: "0.2"}
}

// An opt-in --discover build with no reachable publications must publish the
// exact same local bytes as a default offline build: no candidates and a
// refused private candidate both leave graph.json / llms.txt untouched. The
// private candidate additionally proves the native client never opens a TCP
// connection to a loopback listener (refused before the TLS handshake).
func TestDiscoverBuildMatchesOfflineBytes(t *testing.T) {
	var conns int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0","root":"/index.md"}`))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt64(&conns, 1)
		}
	}
	srv.StartTLS()
	defer srv.Close()

	cases := []struct {
		name          string
		links         []string
		expectRefused bool
	}{
		{name: "no candidates"},
		{name: "refused private", links: []string{srv.URL + "/graph.json"}, expectRefused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, b := discoverProject(t, tc.links...)
			dist := filepath.Join(root, "dist")
			baselinePath := filepath.Join(root, ".poolboy", "sources.lock.json")
			if _, err := source.Scan(b, true); err != nil {
				t.Fatalf("establish source baseline: %v", err)
			}
			baselineBefore, err := os.ReadFile(baselinePath)
			if err != nil {
				t.Fatal(err)
			}
			sourcePath := filepath.Join(root, "index.md")
			sourceBefore, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := Build(context.Background(), b, "", BuildOptions{}); err != nil {
				t.Fatalf("offline build: %v", err)
			}
			offline := snapshot(t, dist)
			if err := Build(context.Background(), b, "", BuildOptions{Discover: true}); err != nil {
				t.Fatalf("discover build: %v", err)
			}
			discovered := snapshot(t, dist)
			if !reflect.DeepEqual(offline, discovered) {
				t.Fatalf("opt-in discovery changed published bytes:\noffline=%v\ndiscover=%v", offline, discovered)
			}
			sourceAfter, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(sourceBefore, sourceAfter) {
				t.Fatalf("opt-in discovery changed accepted source bytes")
			}
			baselineAfter, err := os.ReadFile(baselinePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(baselineBefore, baselineAfter) {
				t.Fatalf("opt-in discovery changed accepted source baseline")
			}
			if bytes.Contains(discovered["graph.json"], []byte("external_references")) {
				t.Fatalf("published graph.json leaked external references: %s", discovered["graph.json"])
			}
			if tc.expectRefused && atomic.LoadInt64(&conns) != 0 {
				t.Fatalf("native client opened %d TCP connection(s) to a private address before refusing", conns)
			}
		})
	}
}

// An opt-in --discover build whose source admission fails must still publish
// the exact local bytes of the prior offline build while surfacing the failure
// honestly. A directory named .gitignore is a non-regular ignore resource, so
// the source link scan rejects it instead of silently skipping admission; a
// private corpus candidate is refused before any connection. Both reach the
// diagnostics sink, and neither suppresses the warning nor corrupts the local
// publication.
func TestDiscoverBuildSurfacesSourceAdmissionFailure(t *testing.T) {
	candidate := "https://127.0.0.1/graph.json"

	root, b := discoverProject(t, candidate)
	dist := filepath.Join(root, "dist")
	if err := Build(context.Background(), b, "", BuildOptions{}); err != nil {
		t.Fatalf("offline build: %v", err)
	}
	offline := snapshot(t, dist)

	// A directory named .gitignore is a non-regular ignore resource: the source
	// scan must reject it rather than silently accepting admission.
	if err := os.Mkdir(filepath.Join(root, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}

	var diagnostics bytes.Buffer
	if err := Build(context.Background(), b, "", BuildOptions{Discover: true, Diagnostics: &diagnostics}); err != nil {
		t.Fatalf("discover build despite unavailable source admission: %v", err)
	}
	if got := snapshot(t, dist); !reflect.DeepEqual(offline, got) {
		t.Fatalf("source admission failure changed published bytes:\noffline=%v\ndiscover=%v", offline, got)
	}

	report := diagnostics.String()
	if !strings.Contains(report, ".gitignore") {
		t.Fatalf("diagnostics did not identify the failing .gitignore resource: %q", report)
	}
	if !strings.Contains(report, candidate) {
		t.Fatalf("diagnostics did not identify the refused private candidate %q: %q", candidate, report)
	}
}

// A canceled parent/operator context must abort the build before any file is
// published and leave an already-published corpus — bytes and materialized
// generated Markdown — exactly as the prior successful build left them.
func TestDiscoverBuildCancellationPreservesPublication(t *testing.T) {
	root, b, rendererPath := fixture(t)
	dist := filepath.Join(root, "dist")
	if err := Build(context.Background(), b, rendererPath, BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dist)
	generatedPath := filepath.Join(root, "docs", "reference", "endpoints.md")
	generatedBefore, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Build(ctx, b, rendererPath, BuildOptions{Discover: true}); err == nil {
		t.Fatal("canceled build unexpectedly succeeded")
	}

	if after := snapshot(t, dist); !reflect.DeepEqual(before, after) {
		t.Fatalf("canceled build changed publication:\nbefore=%v\nafter=%v", before, after)
	}
	generatedAfter, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generatedBefore, generatedAfter) {
		t.Fatalf("canceled build changed generated Markdown: %q", generatedAfter)
	}
}

// validExternalReference is the canonical shape every accepted remote
// publication reference must take.
func validExternalReference() graphExternalReference {
	return graphExternalReference{
		URL:    "https://example.com/corpus/graph.json",
		SHA256: strings.Repeat("a", sha256.Size*2),
		Root:   "/index.md",
		From:   "/index.md",
		Line:   3,
		Origin: "corpus",
	}
}
func externalReferenceForResource(data []byte) graphExternalReference {
	r := validExternalReference()
	sum := sha256.Sum256(data)
	r.SHA256 = hex.EncodeToString(sum[:])
	return r
}

// Remote references embedded in an existing graph.json are re-validated before
// that output may be replaced. A supported reference is accepted; a malformed
// URL, provenance, root, hash, or over-count set is rejected. Each case runs
// through the real validator over a complete publication and verifies that
// validation never mutates the prior output.
func TestValidateExistingPublicationExternalReferences(t *testing.T) {
	type referenceCase struct {
		name string
		refs func() []graphExternalReference
	}
	mutate := func(fn func(*graphExternalReference)) []graphExternalReference {
		r := validExternalReference()
		fn(&r)
		return []graphExternalReference{r}
	}
	cases := []referenceCase{
		{name: "plain-http scheme", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.URL = "http://example.com/corpus/graph.json" })
		}},
		{name: "query string", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.URL = "https://example.com/corpus/graph.json?v=1" })
		}},
		{name: "fragment", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.URL = "https://example.com/corpus/graph.json#x" })
		}},
		{name: "userinfo", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.URL = "https://user@example.com/corpus/graph.json" })
		}},
		{name: "not graph.json", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.URL = "https://example.com/corpus/index.json" })
		}},
		{name: "uncleaned path", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.URL = "https://example.com/corpus/../graph.json" })
		}},
		{name: "wrong root", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.Root = "/other.md" })
		}},
		{name: "short hash", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.SHA256 = strings.Repeat("a", 10) })
		}},
		{name: "uppercase hash", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.SHA256 = strings.Repeat("A", sha256.Size*2) })
		}},
		{name: "non-hex hash", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.SHA256 = strings.Repeat("z", sha256.Size*2) })
		}},
		{name: "empty provenance", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.From = "" })
		}},
		{name: "traversal provenance", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.From = "../escape.md" })
		}},
		{name: "control provenance", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.From = "/bad\nline.md" })
		}},
		{name: "bad origin", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.Origin = "remote" })
		}},
		{name: "zero line", refs: func() []graphExternalReference {
			return mutate(func(r *graphExternalReference) { r.Line = 0 })
		}},
		{name: "duplicate URL", refs: func() []graphExternalReference {
			r := validExternalReference()
			return []graphExternalReference{r, r}
		}},
		{name: "out of order", refs: func() []graphExternalReference {
			a := validExternalReference()
			a.From = "/b.md"
			b := validExternalReference()
			b.URL = "https://example.com/other/graph.json"
			b.From = "/a.md"
			return []graphExternalReference{a, b}
		}},
		{name: "over count", refs: func() []graphExternalReference {
			refs := make([]graphExternalReference, maxExternalReferences+1)
			for i := range refs {
				r := validExternalReference()
				r.URL = "https://example.com/corpus/" + strings.Repeat("x", i+1) + "/graph.json"
				r.Line = i + 1
				refs[i] = r
			}
			return refs
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, b := discoverProject(t)
			if err := Build(context.Background(), b, "", BuildOptions{}); err != nil {
				t.Fatal(err)
			}
			dist := filepath.Join(root, "dist")
			injectExternalReferences(t, dist, tc.refs())
			before := snapshot(t, dist)
			if err := validateExistingPublication(dist); err == nil {
				t.Fatal("malformed existing external reference accepted")
			}
			after := snapshot(t, dist)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("validation changed existing publication:\nbefore=%v\nafter=%v", before, after)
			}
		})
	}

	t.Run("accepts valid reference", func(t *testing.T) {
		root, b := discoverProject(t)
		if err := Build(context.Background(), b, "", BuildOptions{}); err != nil {
			t.Fatal(err)
		}
		dist := filepath.Join(root, "dist")
		injectExternalReferences(t, dist, []graphExternalReference{validExternalReference()})
		if err := validateExistingPublication(dist); err != nil {
			t.Fatalf("valid existing external reference rejected: %v", err)
		}
	})
}

// injectExternalReferences rewrites an existing publication's graph.json to
// carry refs, preserving every other field (and thus the on-disk hashes the
// validator checks against). It returns the bytes written.
func injectExternalReferences(t *testing.T, dist string, refs []graphExternalReference) []byte {
	t.Helper()
	path := filepath.Join(dist, "graph.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var g graph
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	g.ExternalReferences = refs
	out, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

// A rebuild over an existing publication whose graph.json carries a malformed
// remote reference must refuse (OUTPUT_OCCUPIED) and leave that prior output
// in place rather than deleting or regenerating it.
func TestBuildRefusesExistingPublicationWithBadExternalReference(t *testing.T) {
	root, b := discoverProject(t)
	dist := filepath.Join(root, "dist")
	if err := Build(context.Background(), b, "", BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	bad := validExternalReference()
	bad.Origin = "remote"
	injectExternalReferences(t, dist, []graphExternalReference{bad})
	before := snapshot(t, dist)

	err := Build(context.Background(), b, "", BuildOptions{})
	var publicationErr *PublicationError
	if err == nil || !errors.As(err, &publicationErr) || publicationErr.Code != "OUTPUT_OCCUPIED" || publicationErr.Path != dist {
		t.Fatalf("rebuild over bad existing reference error = %v", err)
	}
	after := snapshot(t, dist)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("refused rebuild altered prior output:\nbefore=%v\nafter=%v", before, after)
	}
}

// externalLLMSTarget turns a graph URL into its sibling llms.txt entry point,
// dropping any query or fragment and handling a root-level graph.json.
func TestExternalLLMSTarget(t *testing.T) {
	cases := map[string]string{
		"https://example.com/corpus/graph.json":       "https://example.com/corpus/llms.txt",
		"https://example.com/graph.json":              "https://example.com/llms.txt",
		"https://example.com/a/b/graph.json?v=1#frag": "https://example.com/a/b/llms.txt",
	}
	for in, want := range cases {
		if got := externalLLMSTarget(in); got != want {
			t.Fatalf("externalLLMSTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

// graph.llms must hash exactly the published llms.txt bytes, and a corpus.zip
// artifact must carry only local Markdown — remote references never become
// files, artifacts, or ZIP members.
func TestGraphExternalReferencesStayRemote(t *testing.T) {
	dir := t.TempDir()
	indexBytes := []byte("---\nokf_version: \"0.2\"\n---\n# Root\n")
	if err := os.WriteFile(filepath.Join(dir, "index.md"), indexBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	b := &bundle.Bundle{Root: dir, Dir: dir, Name: "Corp", Spec: "0.2"}
	idx, err := stageIndex(b, dir)
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]document{"/index.md": {path: "/index.md", data: indexBytes}}
	zipBytes, err := buildMarkdownZIP(docs)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := map[string][]byte{"corpus.zip": zipBytes}
	remoteGraph := []byte(`{"version":"0","root":"/index.md","files":{}}`)
	ref := externalReferenceForResource(remoteGraph)
	llms := renderLLMS("Corp", nil, ref)
	graphBytes, err := buildGraph(idx, docs, artifacts, llms, nil, ref)
	if err != nil {
		t.Fatal(err)
	}

	var g graph
	if err := json.Unmarshal(graphBytes, &g); err != nil {
		t.Fatal(err)
	}
	llmsSum := sha256.Sum256(llms)
	wantLLMS := hex.EncodeToString(llmsSum[:])
	if g.LLMS.SHA256 != wantLLMS || g.LLMS.Bytes != len(llms) {
		t.Fatalf("graph.llms does not hash published llms.txt: got %q/%d want %q/%d", g.LLMS.SHA256, g.LLMS.Bytes, wantLLMS, len(llms))
	}
	zipSum := sha256.Sum256(zipBytes)
	zipEntry := g.Artifacts["corpus.zip"]
	if zipEntry.Bytes != len(zipBytes) || zipEntry.SHA256 != hex.EncodeToString(zipSum[:]) {
		t.Fatalf("graph corpus.zip entry does not hash the published archive: got %q/%d", zipEntry.SHA256, zipEntry.Bytes)
	}
	if len(g.ExternalReferences) != 1 || g.ExternalReferences[0].URL != ref.URL || g.ExternalReferences[0].SHA256 != ref.SHA256 {
		t.Fatalf("remote reference missing from graph: %+v", g.ExternalReferences)
	}
	if !strings.Contains(string(llms), ref.SHA256) {
		t.Fatalf("llms.txt omitted the fetched graph resource hash %q", ref.SHA256)
	}
	if len(g.Files) != 1 || g.Files["/index.md"].Bytes != len(indexBytes) {
		t.Fatalf("remote reference changed local file ownership: %+v", g.Files)
	}
	if len(g.Artifacts) != 1 || g.Artifacts["corpus.zip"].Bytes != len(zipBytes) {
		t.Fatalf("remote reference changed local artifact ownership: %+v", g.Artifacts)
	}
	for path := range g.Files {
		if strings.Contains(path, "example.com") {
			t.Fatalf("remote reference leaked into local files: %s", path)
		}
	}
	for path := range g.Artifacts {
		if strings.Contains(path, "example.com") {
			t.Fatalf("remote reference leaked into artifacts: %s", path)
		}
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
		if strings.Contains(f.Name, "example.com") || strings.Contains(f.Name, "graph.json") {
			t.Fatalf("corpus.zip carried a non-local member: %s", f.Name)
		}
	}
	if !reflect.DeepEqual(names, []string{"index.md"}) {
		t.Fatalf("corpus.zip members = %v, want only local Markdown", names)
	}
}
