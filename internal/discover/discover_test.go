package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
	"time"
)

type fixtureTransport map[string][]byte

func (f fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, ok := f[req.URL.String()]
	status := http.StatusOK
	if !ok {
		body = []byte("missing")
		status = http.StatusNotFound
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

type interruptedTransport struct {
	fixtureTransport
	url string
}

func (t interruptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != t.url {
		return t.fixtureTransport.RoundTrip(req)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body: io.NopCloser(io.MultiReader(
			bytes.NewReader([]byte("https://broken.test/graph.json\n")),
			iotest.ErrReader(io.ErrUnexpectedEOF),
		)),
		Request: req,
	}, nil
}

type countingTransport struct {
	responses map[string][]byte
	calls     map[string]int
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls[req.URL.String()]++
	body, ok := t.responses[req.URL.String()]
	status := http.StatusOK
	if !ok {
		body = []byte("missing")
		status = http.StatusNotFound
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

type redirectTransport string

func (t redirectTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	destination, _ := url.Parse(string(t))
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader([]byte("redirected"))),
		Request:    &http.Request{URL: destination},
	}, nil
}

func TestDiscoverFollowsLandingPageAndValidatesHashes(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	graph := publicationGraph(root, llms, nil)
	client := &http.Client{Transport: fixtureTransport{
		"https://hub.test/hub/":             []byte(`<a href="https://docs.test/docs/">Docs</a>`),
		"https://docs.test/docs/":           []byte(`<a href="llms.txt">llms.txt</a> <a href="graph.json">graph.json</a>`),
		"https://docs.test/docs/llms.txt":   llms,
		"https://docs.test/docs/graph.json": graph,
		"https://docs.test/docs/index.md":   root,
	}}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://hub.test/hub/", From: "/index.md", Line: 3, Origin: "corpus"}}, Options{MaxFetches: 20}, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].URL != "https://docs.test/docs/graph.json" || refs[0].From != "/index.md" || refs[0].Line != 3 || refs[0].Origin != "corpus" {
		t.Fatalf("references = %#v, diagnostics = %#v", refs, diagnostics)
	}
	if refs[0].SHA256 != digest(graph) || refs[0].Root != "/index.md" {
		t.Fatalf("reference identity = %#v", refs[0])
	}
}

func TestDiscoverRejectsFalseBeaconAndUnsafeURL(t *testing.T) {
	bad := []byte(`{"version":"0","root":"/index.md","files":{"/index.md":{"bytes":1,"sha256":"0000000000000000000000000000000000000000000000000000000000000000"}},"llms":{"bytes":1,"sha256":"0000000000000000000000000000000000000000000000000000000000000000"}}`)
	client := &http.Client{Transport: fixtureTransport{
		"https://bad.test/graph.json": bad,
		"https://bad.test/llms.txt":   []byte("x"),
		"https://bad.test/index.md":   []byte("x"),
	}}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://bad.test/graph.json", From: "src/catalogue.ts", Line: 9, Origin: "source"}}, Options{}, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 || len(diagnostics) == 0 {
		t.Fatalf("false beacon refs=%#v diagnostics=%#v", refs, diagnostics)
	}
	for _, raw := range []string{"http://example.test/", "https://user:pass@example.test/", "data:text/plain,x"} {
		if _, err := validateURL(raw); err == nil {
			t.Errorf("unsafe URL accepted: %s", raw)
		}
	}
	if isPublicIP(net.ParseIP("127.0.0.1")) || isPublicIP(net.ParseIP("169.254.169.254")) {
		t.Error("private or metadata address accepted")
	}
}

func TestDiscoverRejectsUnsafeRedirectDestination(t *testing.T) {
	client := &http.Client{Transport: redirectTransport("http://127.0.0.1/private")}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://safe.test/project/", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{}, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 || len(diagnostics) == 0 {
		t.Fatalf("unsafe redirect refs=%#v diagnostics=%#v", refs, diagnostics)
	}
}

func TestDiscoverDeduplicatesTypedChildrenAndKeepsLocalProvenance(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	childRoot := []byte("# Child\n")
	childLLMS := []byte("# Child docs\n")
	child := publicationGraph(childRoot, childLLMS, nil)
	parent := publicationGraph(root, llms, []childReference{{URL: "https://docs.test/child/graph.json", SHA256: digest(child), Root: "/index.md", From: "/remote.md", Line: 42, Origin: "external"}, {URL: "https://docs.test/child/graph.json", SHA256: digest(child), Root: "/index.md", From: "/remote.md", Line: 42, Origin: "external"}})
	client := &http.Client{Transport: fixtureTransport{
		"https://docs.test/parent/graph.json": parent,
		"https://docs.test/parent/llms.txt":   llms,
		"https://docs.test/parent/index.md":   root,
		"https://docs.test/child/graph.json":  child,
		"https://docs.test/child/llms.txt":    childLLMS,
		"https://docs.test/child/index.md":    childRoot,
	}}
	refs, _, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://docs.test/parent/graph.json", From: "src/catalogue.ts", Line: 12, Origin: "source"}}, Options{}, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].From != "src/catalogue.ts" || refs[1].From != "src/catalogue.ts" || refs[0].Line != 12 || refs[1].Line != 12 {
		t.Fatalf("references = %#v", refs)
	}
}

func publicationGraph(root, llms []byte, children []childReference) []byte {
	rootHash := digest(root)
	llmsHash := digest(llms)
	value := graphManifest{
		Version:            "0",
		Root:               "/index.md",
		LLMS:               graphArtifact{Bytes: len(llms), SHA256: llmsHash},
		Files:              map[string]graphFile{"/index.md": {Bytes: len(root), SHA256: rootHash}},
		ExternalReferences: children,
	}
	data, _ := json.Marshal(value)
	return data
}

func hasDiagnostic(diagnostics []Diagnostic, resource string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.URL == resource {
			return true
		}
	}
	return false
}

func manifestBytes(manifest graphManifest) []byte {
	data, _ := json.Marshal(manifest)
	return data
}

func validManifest(root, llms []byte) graphManifest {
	return graphManifest{
		Version: "0",
		Root:    "/index.md",
		LLMS:    graphArtifact{Bytes: len(llms), SHA256: digest(llms)},
		Files:   map[string]graphFile{"/index.md": {Bytes: len(root), SHA256: digest(root)}},
	}
}

// resolvedIP mirrors the byte form net.Resolver.LookupIP hands safeDialer:
// four bytes for an A record, sixteen for a AAAA record.
func resolvedIP(literal string) net.IP {
	parsed := net.ParseIP(literal)
	if strings.Contains(literal, ":") {
		return parsed
	}
	return parsed.To4()
}

// mappedIPv4 builds the IPv4-mapped IPv6 form a hostile AAAA record can return.
func mappedIPv4(dotted string) net.IP {
	mapped := make(net.IP, net.IPv6len)
	mapped[10], mapped[11] = 0xff, 0xff
	copy(mapped[12:], net.ParseIP(dotted).To4())
	return mapped
}

func TestIsPublicIPClassifiesAddressBoundaries(t *testing.T) {
	public := []string{
		"93.184.216.34",
		"8.8.8.8",
		"2606:2800:220:1:248:1893:25c8:1946",
		"2001:4860:4860::8888",
	}
	blocked := []string{
		"0.0.0.0", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.169.254",
		"172.16.5.4", "192.0.0.1", "192.0.2.1", "192.168.1.1", "198.18.0.1",
		"198.51.100.9", "203.0.113.7", "224.0.0.1", "240.0.0.1",
		"::", "::1", "fc00::1", "fd12::1", "fe80::1", "2001:db8::1", "2001:10::1",
	}
	for _, literal := range public {
		if !isPublicIP(resolvedIP(literal)) {
			t.Errorf("public address rejected: %s", literal)
		}
	}
	for _, literal := range blocked {
		if isPublicIP(resolvedIP(literal)) {
			t.Errorf("non-public address accepted: %s", literal)
		}
	}
}

// An IPv4-mapped IPv6 address must classify exactly like the IPv4 address it
// carries, and the NAT64 special-use prefixes are refused outright rather than
// by interpreting their embedded payload.
func TestIsPublicIPRejectsMappedAndTranslatedReservedRanges(t *testing.T) {
	if !isPublicIP(mappedIPv4("93.184.216.34")) {
		t.Error("mapped public IPv4 rejected")
	}
	for _, dotted := range []string{
		"10.0.0.1", "127.0.0.1", "169.254.169.254", "172.16.5.4", "192.168.1.1",
		"100.64.0.1", "192.0.2.1", "198.18.0.1", "198.51.100.9", "203.0.113.7", "240.0.0.1",
	} {
		if isPublicIP(mappedIPv4(dotted)) {
			t.Errorf("mapped non-public address accepted: ::ffff:%s", dotted)
		}
	}
	for _, literal := range []string{
		"64:ff9b::a00:1", "64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", "64:ff9b::808:808",
		"64:ff9b:1::a00:1", "64:ff9b:1::808:808",
	} {
		if isPublicIP(net.ParseIP(literal)) {
			t.Errorf("NAT64 translated address accepted: %s", literal)
		}
	}
}

func TestDiscoverRejectsMalformedGraphManifests(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	filler := graphFile{Bytes: 1, SHA256: digest([]byte("x"))}
	cases := []struct {
		name  string
		build func() []byte
	}{
		{"unknown version", func() []byte { m := validManifest(root, llms); m.Version = "1"; return manifestBytes(m) }},
		{"empty version", func() []byte { m := validManifest(root, llms); m.Version = ""; return manifestBytes(m) }},
		{"foreign root", func() []byte {
			m := validManifest(root, llms)
			m.Root = "/other.md"
			m.Files = map[string]graphFile{"/other.md": {Bytes: len(root), SHA256: digest(root)}}
			return manifestBytes(m)
		}},
		{"traversal file path", func() []byte {
			m := validManifest(root, llms)
			m.Files["/../secret.md"] = filler
			return manifestBytes(m)
		}},
		{"dot segment file path", func() []byte {
			m := validManifest(root, llms)
			m.Files["/docs/./a.md"] = filler
			return manifestBytes(m)
		}},
		{"backslash file path", func() []byte {
			m := validManifest(root, llms)
			m.Files[`/docs\a.md`] = filler
			return manifestBytes(m)
		}},
		{"fragment file path", func() []byte {
			m := validManifest(root, llms)
			m.Files["/docs/a.md#frag"] = filler
			return manifestBytes(m)
		}},
		{"query file path", func() []byte {
			m := validManifest(root, llms)
			m.Files["/docs/a.md?x=1"] = filler
			return manifestBytes(m)
		}},
		{"relative file path", func() []byte {
			m := validManifest(root, llms)
			m.Files["docs/a.md"] = filler
			return manifestBytes(m)
		}},
		{"non-markdown file", func() []byte {
			m := validManifest(root, llms)
			m.Files["/docs/data.txt"] = filler
			return manifestBytes(m)
		}},
		{"invalid file hash", func() []byte {
			m := validManifest(root, llms)
			m.Files["/index.md"] = graphFile{Bytes: len(root), SHA256: "not-a-digest"}
			return manifestBytes(m)
		}},
		{"uppercase file hash", func() []byte {
			m := validManifest(root, llms)
			m.Files["/index.md"] = graphFile{Bytes: len(root), SHA256: strings.ToUpper(digest(root))}
			return manifestBytes(m)
		}},
		{"zero byte file", func() []byte {
			m := validManifest(root, llms)
			m.Files["/index.md"] = graphFile{Bytes: 0, SHA256: digest(root)}
			return manifestBytes(m)
		}},
		{"negative byte file", func() []byte {
			m := validManifest(root, llms)
			m.Files["/index.md"] = graphFile{Bytes: -1, SHA256: digest(root)}
			return manifestBytes(m)
		}},
		{"no files", func() []byte {
			m := validManifest(root, llms)
			m.Files = map[string]graphFile{}
			return manifestBytes(m)
		}},
		{"invalid llms hash", func() []byte {
			m := validManifest(root, llms)
			m.LLMS = graphArtifact{Bytes: len(llms), SHA256: "zz"}
			return manifestBytes(m)
		}},
		{"trailing json", func() []byte {
			return append(manifestBytes(validManifest(root, llms)), []byte(" {}")...)
		}},
		{"not json", func() []byte { return []byte("<html>not a publication</html>") }},
		{"child foreign root", func() []byte {
			m := validManifest(root, llms)
			m.ExternalReferences = []childReference{{URL: "https://child.test/graph.json", Root: "/elsewhere.md", SHA256: digest([]byte("c"))}}
			return manifestBytes(m)
		}},
		{"child invalid hash", func() []byte {
			m := validManifest(root, llms)
			m.ExternalReferences = []childReference{{URL: "https://child.test/graph.json", Root: "/index.md", SHA256: "short"}}
			return manifestBytes(m)
		}},
		{"child empty url", func() []byte {
			m := validManifest(root, llms)
			m.ExternalReferences = []childReference{{URL: "   ", Root: "/index.md", SHA256: digest([]byte("c"))}}
			return manifestBytes(m)
		}},
		{"child negative line", func() []byte {
			m := validManifest(root, llms)
			m.ExternalReferences = []childReference{{URL: "https://child.test/graph.json", Root: "/index.md", SHA256: digest([]byte("c")), Line: -1}}
			return manifestBytes(m)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &http.Client{Transport: fixtureTransport{
				"https://corp.test/graph.json": testCase.build(),
				"https://corp.test/llms.txt":   llms,
				"https://corp.test/index.md":   root,
			}}
			refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/graph.json", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{}, client)
			if err != nil {
				t.Fatal(err)
			}
			if len(refs) != 0 {
				t.Fatalf("invalid manifest accepted: %#v", refs)
			}
			if len(diagnostics) == 0 {
				t.Fatal("invalid manifest rejected without a diagnostic")
			}
		})
	}
}

// A publication Poolboy itself can emit must be accepted: documents up to the
// local Markdown ceiling, and canonical paths holding a literal percent.
func TestDiscoverAcceptsLargeDocumentMetadataAndLiteralPercentPaths(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	manifest := validManifest(root, llms)
	manifest.Files["/report%20draft.md"] = graphFile{Bytes: 64, SHA256: digest([]byte("draft"))}
	manifest.Files["/big.md"] = graphFile{Bytes: 6 << 20, SHA256: digest([]byte("big"))}
	graph := manifestBytes(manifest)
	client := &http.Client{Transport: fixtureTransport{
		"https://corp.test/graph.json": graph,
		"https://corp.test/llms.txt":   llms,
		"https://corp.test/index.md":   root,
	}}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/graph.json", From: "/index.md", Line: 2, Origin: "corpus"}}, Options{}, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].SHA256 != digest(graph) {
		t.Fatalf("valid publication rejected: refs=%#v diagnostics=%#v", refs, diagnostics)
	}
}

func TestDiscoverRejectsDeclarationDigestMismatch(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	graph := publicationGraph(root, llms, nil)
	cases := []struct {
		name     string
		llmsBody []byte
		rootBody []byte
	}{
		{"llms byte count", []byte("tampered llms payload"), root},
		{"llms digest at equal length", []byte("# DOCS\n"), root},
		{"root byte count", llms, []byte("tampered root payload")},
		{"root digest at equal length", llms, []byte("# ROOT\n")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &http.Client{Transport: fixtureTransport{
				"https://corp.test/graph.json": graph,
				"https://corp.test/llms.txt":   testCase.llmsBody,
				"https://corp.test/index.md":   testCase.rootBody,
			}}
			refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/graph.json", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{}, client)
			if err != nil {
				t.Fatal(err)
			}
			if len(refs) != 0 || !hasDiagnostic(diagnostics, "https://corp.test/graph.json") {
				t.Fatalf("digest mismatch admitted: refs=%#v diagnostics=%#v", refs, diagnostics)
			}
		})
	}
}

func TestDiscoverRejectsPinnedChildHashMismatch(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	childRoot := []byte("# Child\n")
	childLLMS := []byte("# Child docs\n")
	child := publicationGraph(childRoot, childLLMS, nil)
	parent := publicationGraph(root, llms, []childReference{{
		URL: "https://corp.test/child/graph.json", SHA256: digest([]byte("a different publication")),
		Root: "/index.md", From: "/remote.md", Line: 7, Origin: "external",
	}})
	client := &http.Client{Transport: fixtureTransport{
		"https://corp.test/parent/graph.json": parent,
		"https://corp.test/parent/llms.txt":   llms,
		"https://corp.test/parent/index.md":   root,
		"https://corp.test/child/graph.json":  child,
		"https://corp.test/child/llms.txt":    childLLMS,
		"https://corp.test/child/index.md":    childRoot,
	}}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/parent/graph.json", From: "src/catalogue.ts", Line: 3, Origin: "source"}}, Options{}, client)
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range refs {
		if reference.URL == "https://corp.test/child/graph.json" {
			t.Fatalf("child admitted despite pin mismatch: %#v", refs)
		}
	}
	if !hasDiagnostic(diagnostics, "https://corp.test/child/graph.json") {
		t.Fatalf("missing pin mismatch diagnostic: %#v", diagnostics)
	}
}

// A graph published independently at an early local candidate is validated and
// returned on its own digest. When a second valid parent, declared at a later
// candidate, pins that same graph URL with a conflicting valid-format SHA, both
// publications must remain returned on their actual digests, the already
// validated graph must not be fetched again, and the conflicting pin must be
// surfaced as a diagnostic naming the graph's own URL.
func TestDiscoverReportsConflictingPinOnIndependentlyValidGraph(t *testing.T) {
	bRoot, bLLMS := []byte("# Beta\n"), []byte("# Beta docs\n")
	aRoot, aLLMS := []byte("# Alpha\n"), []byte("# Alpha docs\n")
	bGraph := publicationGraph(bRoot, bLLMS, nil)
	aGraph := publicationGraph(aRoot, aLLMS, []childReference{{
		URL: "https://beta.test/graph.json", SHA256: digest([]byte("a different beta publication")),
		Root: "/index.md", From: "/remote.md", Line: 9, Origin: "external",
	}})
	transport := &countingTransport{
		responses: map[string][]byte{
			"https://beta.test/graph.json":  bGraph,
			"https://beta.test/llms.txt":    bLLMS,
			"https://beta.test/index.md":    bRoot,
			"https://alpha.test/graph.json": aGraph,
			"https://alpha.test/llms.txt":   aLLMS,
			"https://alpha.test/index.md":   aRoot,
		},
		calls: map[string]int{},
	}
	// Beta's declaring path (/a.md) sorts before alpha's (/b.md), so the direct
	// beta candidate is visited and validated before alpha's typed child for
	// beta is ever queued.
	candidates := []Candidate{
		{URL: "https://beta.test/graph.json", From: "/a.md", Line: 1, Origin: "corpus"},
		{URL: "https://alpha.test/graph.json", From: "/b.md", Line: 1, Origin: "corpus"},
	}
	refs, diagnostics, err := discoverWithClient(context.Background(), candidates, Options{}, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	var alpha, beta *ExternalReference
	for i := range refs {
		switch refs[i].URL {
		case "https://alpha.test/graph.json":
			alpha = &refs[i]
		case "https://beta.test/graph.json":
			beta = &refs[i]
		}
	}
	if alpha == nil || beta == nil {
		t.Fatalf("independent publications dropped: refs=%#v", refs)
	}
	if beta.SHA256 != digest(bGraph) {
		t.Fatalf("beta returned on a hash other than its own fetched graph: %#v", beta)
	}
	if !hasDiagnostic(diagnostics, "https://beta.test/graph.json") {
		t.Fatalf("conflicting pin on beta was not surfaced: diagnostics=%#v", diagnostics)
	}
	for _, resource := range []string{
		"https://beta.test/graph.json", "https://beta.test/llms.txt", "https://beta.test/index.md",
		"https://alpha.test/graph.json", "https://alpha.test/llms.txt", "https://alpha.test/index.md",
	} {
		if transport.calls[resource] != 1 {
			t.Fatalf("resource %s fetched %d times; cache contract requires exactly one", resource, transport.calls[resource])
		}
	}
}

// A typed child reference aimed at a landing page rather than a graph.json
// must not be followed: ordinary page traversal drops the declared hash, which
// would admit the child publication unpinned.
func TestDiscoverSkipsChildReferenceOutsideGraphJSON(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	childRoot := []byte("# Child\n")
	childLLMS := []byte("# Child docs\n")
	child := publicationGraph(childRoot, childLLMS, nil)
	parent := publicationGraph(root, llms, []childReference{{
		URL: "https://corp.test/child/", SHA256: digest(child),
		Root: "/index.md", From: "/remote.md", Line: 7, Origin: "external",
	}})
	client := &http.Client{Transport: fixtureTransport{
		"https://corp.test/parent/graph.json": parent,
		"https://corp.test/parent/llms.txt":   llms,
		"https://corp.test/parent/index.md":   root,
		"https://corp.test/child/":            []byte(`<a href="graph.json">graph.json</a>`),
		"https://corp.test/child/graph.json":  child,
		"https://corp.test/child/llms.txt":    childLLMS,
		"https://corp.test/child/index.md":    childRoot,
	}}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/parent/graph.json", From: "src/catalogue.ts", Line: 3, Origin: "source"}}, Options{}, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].URL != "https://corp.test/parent/graph.json" {
		t.Fatalf("non-graph child reference followed: refs=%#v", refs)
	}
	if !hasDiagnostic(diagnostics, "https://corp.test/parent/graph.json") {
		t.Fatalf("missing child target diagnostic: %#v", diagnostics)
	}
}

func TestDiscoverBudgetsStopTraversalAndReportPartialResults(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	graph := publicationGraph(root, llms, nil)

	t.Run("fetch budget", func(t *testing.T) {
		client := &http.Client{Transport: fixtureTransport{
			"https://corp.test/p/":           []byte(`<a href="graph.json">graph.json</a>`),
			"https://corp.test/p/graph.json": graph,
			"https://corp.test/p/llms.txt":   llms,
			"https://corp.test/p/index.md":   root,
		}}
		refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/p/", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{MaxFetches: 1}, client)
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) != 0 {
			t.Fatalf("fetch budget overrun: %#v", refs)
		}
		if len(diagnostics) == 0 {
			t.Fatalf("fetch budget not reported: %#v", diagnostics)
		}
	})

	t.Run("reference budget caps exactly", func(t *testing.T) {
		fixtures := fixtureTransport{}
		children := make([]childReference, 0, 3)
		for _, name := range []string{"a", "b", "c"} {
			childRoot := []byte("# " + name + "\n")
			childLLMS := []byte("# " + name + " docs\n")
			childGraph := publicationGraph(childRoot, childLLMS, nil)
			base := "https://corp.test/" + name + "/"
			fixtures[base+"graph.json"] = childGraph
			fixtures[base+"llms.txt"] = childLLMS
			fixtures[base+"index.md"] = childRoot
			children = append(children, childReference{URL: base + "graph.json", SHA256: digest(childGraph), Root: "/index.md", From: "/remote.md", Line: 1, Origin: "external"})
		}
		parent := publicationGraph(root, llms, children)
		fixtures["https://corp.test/parent/graph.json"] = parent
		fixtures["https://corp.test/parent/llms.txt"] = llms
		fixtures["https://corp.test/parent/index.md"] = root
		refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/parent/graph.json", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{MaxReferences: 2, MaxFetches: 64}, &http.Client{Transport: fixtures})
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) != 2 {
			t.Fatalf("reference budget produced %d references, want exactly 2: %#v", len(refs), refs)
		}
		if len(diagnostics) == 0 {
			t.Fatalf("reference budget not reported: %#v", diagnostics)
		}
	})

	t.Run("aggregate byte budget", func(t *testing.T) {
		client := &http.Client{Transport: fixtureTransport{"https://corp.test/p/": []byte("a landing page body")}}
		refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/p/", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{AggregateBytes: 4}, client)
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) != 0 || len(diagnostics) == 0 {
			t.Fatalf("aggregate budget not reported: refs=%#v diagnostics=%#v", refs, diagnostics)
		}
	})

	t.Run("per response byte limit", func(t *testing.T) {
		client := &http.Client{Transport: fixtureTransport{"https://corp.test/p/": []byte(strings.Repeat("x", 512))}}
		refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/p/", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{ResponseBytes: 8}, client)
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) != 0 || len(diagnostics) == 0 {
			t.Fatalf("response limit not reported: refs=%#v diagnostics=%#v", refs, diagnostics)
		}
	})

	t.Run("depth limit", func(t *testing.T) {
		client := &http.Client{Transport: fixtureTransport{
			"https://corp.test/a/":           []byte(`<a href="https://corp.test/b/">b</a>`),
			"https://corp.test/b/":           []byte(`<a href="https://corp.test/c/">c</a>`),
			"https://corp.test/c/":           []byte(`<a href="graph.json">graph.json</a>`),
			"https://corp.test/c/graph.json": graph,
			"https://corp.test/c/llms.txt":   llms,
			"https://corp.test/c/index.md":   root,
		}}
		refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/a/", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{MaxDepth: 1, MaxFetches: 64}, client)
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) != 0 || len(diagnostics) == 0 {
			t.Fatalf("depth limit not enforced: refs=%#v diagnostics=%#v", refs, diagnostics)
		}
	})
}

func TestDiscoverSeedFairnessKeepsPerSeedProvenance(t *testing.T) {
	type seedFixture struct {
		host   string
		from   string
		line   int
		origin string
	}
	seeds := []seedFixture{
		{"alpha.test", "src/catalogue.ts", 20, "source"},
		{"beta.test", "/index.md", 4, "corpus"},
		{"gamma.test", "/docs/links.md", 11, "corpus"},
	}
	fixtures := fixtureTransport{}
	candidates := make([]Candidate, 0, len(seeds))
	for _, seed := range seeds {
		seedRoot := []byte("# " + seed.host + "\n")
		seedLLMS := []byte("# " + seed.host + " docs\n")
		base := "https://" + seed.host + "/"
		fixtures[base+"graph.json"] = publicationGraph(seedRoot, seedLLMS, nil)
		fixtures[base+"llms.txt"] = seedLLMS
		fixtures[base+"index.md"] = seedRoot
		candidates = append(candidates, Candidate{URL: base + "graph.json", From: seed.from, Line: seed.line, Origin: seed.origin})
	}
	refs, diagnostics, err := discoverWithClient(context.Background(), candidates, Options{MaxFetches: 64}, &http.Client{Transport: fixtures})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != len(seeds) {
		t.Fatalf("seed starved: refs=%#v diagnostics=%#v", refs, diagnostics)
	}
	byURL := make(map[string]ExternalReference, len(refs))
	for _, reference := range refs {
		byURL[reference.URL] = reference
	}
	for _, seed := range seeds {
		got := byURL["https://"+seed.host+"/graph.json"]
		if got.From != seed.from || got.Line != seed.line || got.Origin != seed.origin {
			t.Errorf("seed %s provenance = %#v, want from=%q line=%d origin=%q", seed.host, got, seed.from, seed.line, seed.origin)
		}
	}
}

// tarpitTransport answers known URLs immediately and stalls every other
// request until its context expires, with no wall-clock sleep of its own.
type tarpitTransport struct{ fast fixtureTransport }

func (t tarpitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if body, ok := t.fast[req.URL.String()]; ok {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    req,
		}, nil
	}
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// An internal time budget is an ordinary remote condition: discovery keeps what
// it validated and reports a diagnostic. Only an operator cancelling the parent
// context is fatal.
func TestDiscoverInternalTimeoutIsPartialAndParentCancelIsFatal(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	fast := fixtureTransport{
		"https://fast.test/graph.json": publicationGraph(root, llms, nil),
		"https://fast.test/llms.txt":   llms,
		"https://fast.test/index.md":   root,
	}
	seeds := []Candidate{
		{URL: "https://fast.test/graph.json", From: "/a.md", Line: 1, Origin: "corpus"},
		{URL: "https://tarpit.test/graph.json", From: "/b.md", Line: 2, Origin: "corpus"},
	}

	t.Run("internal time budget", func(t *testing.T) {
		refs, diagnostics, err := discoverWithClient(context.Background(), seeds, Options{TotalTimeout: time.Second}, &http.Client{Transport: tarpitTransport{fast: fast}})
		if err != nil {
			t.Fatalf("internal time budget returned a fatal error: %v", err)
		}
		if len(refs) != 1 || refs[0].URL != "https://fast.test/graph.json" {
			t.Fatalf("partial results lost: %#v", refs)
		}
		if !hasDiagnostic(diagnostics, "") {
			t.Fatalf("missing time budget diagnostic: %#v", diagnostics)
		}
	})

	t.Run("parent cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := discoverWithClient(ctx, seeds, Options{}, &http.Client{Transport: tarpitTransport{fast: fast}}); !errors.Is(err, context.Canceled) {
			t.Fatalf("parent cancellation error = %v, want context.Canceled", err)
		}
	})
}

func TestExtractLinksResolvesRelativeEntitiesAndFragments(t *testing.T) {
	base, err := url.Parse("https://docs.test/a/b/")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`
[relative](../c/)
[dotted](./d/)
[absolute path](/abs/)
[external](https://other.test/z/)
[duplicate](https://other.test/z/)
[insecure](http://insecure.test/)
<a href="page.md#section">anchored</a>
<a href="https://docs.test/a/b/q/?x=1&amp;y=2">entity</a>
`)
	links, truncated := extractLinks(body, base)
	if truncated {
		t.Fatal("small page reported as truncated")
	}
	seen := make(map[string]int, len(links))
	for _, link := range links {
		seen[link]++
	}
	for _, want := range []string{
		"https://docs.test/a/c/",
		"https://docs.test/a/b/d/",
		"https://docs.test/abs/",
		"https://other.test/z/",
		"https://docs.test/a/b/page.md",
		"https://docs.test/a/b/q/?x=1&y=2",
	} {
		if seen[want] == 0 {
			t.Errorf("missing resolved link %q in %v", want, links)
		}
	}
	if seen["https://other.test/z/"] > 1 {
		t.Errorf("duplicate link retained: %v", links)
	}
	for _, unwanted := range []string{"http://insecure.test/", "https://docs.test/a/b/page.md#section"} {
		if seen[unwanted] != 0 {
			t.Errorf("unsafe or unnormalised link kept: %q in %v", unwanted, links)
		}
	}
}

// Rejected matches must not consume the per-page link budget: a real
// publication link behind a wall of noise is still found, and a page that
// genuinely overflows the budget says so instead of truncating silently.
func TestExtractLinksSurvivesNoiseAndReportsTruncation(t *testing.T) {
	base, err := url.Parse("https://docs.test/")
	if err != nil {
		t.Fatal(err)
	}
	var noisy bytes.Buffer
	for i := range maxPageLinks + 64 {
		fmt.Fprintf(&noisy, "http://insecure%d.test/asset.png\n", i)
	}
	noisy.WriteString("https://real.test/docs/graph.json\n")
	links, truncated := extractLinks(noisy.Bytes(), base)
	if truncated {
		t.Errorf("rejected matches counted against the link budget: %d links", len(links))
	}
	found := false
	for _, link := range links {
		if link == "https://real.test/docs/graph.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("publication link hidden behind rejected matches: %v", links)
	}

	var dense bytes.Buffer
	for i := range maxPageLinks + 64 {
		fmt.Fprintf(&dense, "https://h%d.test/docs/\n", i)
	}
	capped, denseTruncated := extractLinks(dense.Bytes(), base)
	if len(capped) != maxPageLinks || !denseTruncated {
		t.Fatalf("link cap = %d links truncated=%v, want %d and true", len(capped), denseTruncated, maxPageLinks)
	}
}

func TestDiscoverReportsPageLinkBudget(t *testing.T) {
	var dense bytes.Buffer
	for i := range maxPageLinks + 8 {
		fmt.Fprintf(&dense, "<a href=\"https://corp.test/p/d%d/\">d</a>\n", i)
	}
	client := &http.Client{Transport: fixtureTransport{"https://corp.test/p/": dense.Bytes()}}
	_, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/p/", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{MaxFetches: 2}, client)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(diagnostics, "https://corp.test/p/") {
		t.Fatalf("silent link truncation: %#v", diagnostics)
	}
}

// Discovery follows the real chain source -> seed page -> adopted project
// entrypoint -> advertised landing -> manifest, and nothing else. The adopted
// entrypoint is scoped to its own path prefix on a segment boundary, so
// global navigation above it and a sibling path that merely shares a prefix
// are both refused; script, JSON-LD, image, license and tracker URLs are never
// hyperlinks; and no graph.json or llms.txt beacon is guessed at a base that
// never advertised one.
func TestDiscoverDoesNotOvercrawlUnrelatedLinks(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	graph := publicationGraph(root, llms, nil)
	seedPage := []byte(`<!doctype html>
<script type="application/ld+json">{"@context":"https://schema.org","url":"https://schema.org/WebSite","image":"https://cdn.test/hero.png"}</script>
<script src="https://cdn.test/analytics.js"></script>
<img src="https://cdn.test/badge.png">
<a href="https://github.test/org/repo">Project</a>
`)
	entrypoint := []byte(`<!doctype html>
<a href="https://github.test/features">Features</a>
<a href="https://github.test/pricing">Pricing</a>
<a href="https://github.test/org/repo2/evil/">Lookalike</a>
<a href="https://opensource.test/licenses/mit">MIT</a>
<a href="https://tracker.test/issues">Issues</a>
<a href="https://github.test/org/repo/docs/">Documentation</a>
`)
	landing := []byte(`<!doctype html>
<a href="llms.txt">llms.txt</a>
<a href="graph.json">graph.json</a>
`)
	transport := &countingTransport{
		responses: map[string][]byte{
			"https://app.test/docs/":                       seedPage,
			"https://github.test/org/repo":                 entrypoint,
			"https://github.test/org/repo/docs/":           landing,
			"https://github.test/org/repo/docs/graph.json": graph,
			"https://github.test/org/repo/docs/llms.txt":   llms,
			"https://github.test/org/repo/docs/index.md":   root,
		},
		calls: map[string]int{},
	}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://app.test/docs/", From: "src/catalogue.ts", Line: 5, Origin: "source"}}, Options{MaxFetches: 64}, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].URL != "https://github.test/org/repo/docs/graph.json" {
		t.Fatalf("published chain not discovered: refs=%#v diagnostics=%#v", refs, diagnostics)
	}
	if refs[0].From != "src/catalogue.ts" || refs[0].Line != 5 || refs[0].Origin != "source" {
		t.Errorf("local provenance lost across the chain: %#v", refs[0])
	}
	for _, reached := range []string{
		"https://app.test/docs/",
		"https://github.test/org/repo",
		"https://github.test/org/repo/docs/",
		"https://github.test/org/repo/docs/graph.json",
		"https://github.test/org/repo/docs/llms.txt",
		"https://github.test/org/repo/docs/index.md",
	} {
		if transport.calls[reached] == 0 {
			t.Errorf("expected request was never made: %s", reached)
		}
	}
	for _, forbidden := range []string{
		"https://schema.org/WebSite",
		"https://cdn.test/hero.png",
		"https://cdn.test/analytics.js",
		"https://cdn.test/badge.png",
		"https://opensource.test/licenses/mit",
		"https://tracker.test/issues",
		"https://github.test/features",
		"https://github.test/pricing",
		"https://github.test/org/repo2/evil/",
		"https://github.test/graph.json",
		"https://github.test/llms.txt",
		"https://github.test/org/graph.json",
		"https://app.test/docs/graph.json",
		"https://app.test/docs/llms.txt",
	} {
		if transport.calls[forbidden] != 0 {
			t.Errorf("overcrawled %s (%d requests); calls=%v", forbidden, transport.calls[forbidden], transport.calls)
		}
	}
}

// A hostname that resolves only to non-public addresses must be refused before
// the dialer's socket hook runs. The controlled public dial exercises the
// pinned-address error path without contacting the network.
func TestSafeDialerRefusesNonPublicResolutionBeforeAnyConnection(t *testing.T) {
	ctx := context.Background()
	privateControlCalls := 0
	refusing := &safeDialer{
		resolver: &net.Resolver{},
		dialer: &net.Dialer{
			Timeout: time.Second,
			Control: func(_, _ string, _ syscall.RawConn) error {
				privateControlCalls++
				return errors.New("unexpected socket")
			},
		},
		pins: map[string][]net.IP{},
	}
	for _, host := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "64:ff9b::c000:201"} {
		conn, err := refusing.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
		if err == nil {
			if conn != nil {
				_ = conn.Close()
			}
			t.Fatalf("non-public address was allowed: %s", host)
		}
	}
	if privateControlCalls != 0 {
		t.Fatalf("non-public dial reached socket control %d times", privateControlCalls)
	}
	if _, err := refusing.DialContext(ctx, "udp", "127.0.0.1:443"); err == nil {
		t.Error("a non-tcp network was allowed")
	}
	if _, err := refusing.DialContext(ctx, "tcp", "missing-port"); err == nil {
		t.Error("an address without a port was allowed")
	}

	controlErr := errors.New("controlled dial refusal")
	controlCalls := 0
	controlled := &safeDialer{
		resolver: &net.Resolver{},
		dialer: &net.Dialer{
			Timeout: time.Second,
			Control: func(_, _ string, _ syscall.RawConn) error {
				controlCalls++
				return controlErr
			},
		},
		pins: map[string][]net.IP{},
	}
	for range 2 {
		if _, err := controlled.DialContext(ctx, "tcp", "1.1.1.1:443"); !errors.Is(err, controlErr) {
			t.Fatalf("controlled public dial error = %v, want %v", err, controlErr)
		}
	}
	if controlCalls != 2 {
		t.Fatalf("public dial control calls = %d, want 2 (including cached pin)", controlCalls)
	}
}

// redirectRouteTransport returns a 302 to location[url] when present,
// otherwise a 200 carrying body[url]. It drives the production CheckRedirect
// policy while keeping responses deterministic and offline.
type redirectRouteTransport struct {
	location map[string]string
	body     map[string][]byte
}

func (t *redirectRouteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	if loc, ok := t.location[u]; ok {
		header := make(http.Header)
		header.Set("Location", loc)
		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     header,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    req,
		}, nil
	}
	body := t.body[u]
	if body == nil {
		body = []byte("ok")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

// fetch must follow a safe same-scheme redirect to its final resource but let
// the production CheckRedirect policy refuse a scheme downgrade, a
// destination carrying credentials, and a chain longer than the redirect
// budget.
func TestFetchFollowsSafeRedirectsAndRefusesUnsafeOnes(t *testing.T) {
	start := &url.URL{Scheme: "https", Host: "a.test", Path: "/graph.json"}

	t.Run("legitimate https redirect is followed", func(t *testing.T) {
		e := newEngine(withDefaults(Options{MaxFetches: 20}))
		e.client.Transport = &redirectRouteTransport{
			location: map[string]string{"https://a.test/graph.json": "https://b.test/graph.json"},
			body:     map[string][]byte{"https://b.test/graph.json": []byte("moved-body")},
		}
		final, body, err := e.fetch(context.Background(), start, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if final != "https://b.test/graph.json" || string(body) != "moved-body" {
			t.Fatalf("redirect not followed to final resource: final=%q body=%q", final, body)
		}
	})

	t.Run("scheme downgrade is refused", func(t *testing.T) {
		e := newEngine(withDefaults(Options{MaxFetches: 20}))
		e.client.Transport = &redirectRouteTransport{
			location: map[string]string{"https://a.test/graph.json": "http://a.test/graph.json"},
		}
		if final, _, err := e.fetch(context.Background(), start, 1<<20); err == nil {
			t.Fatalf("redirect to http was allowed: final=%q", final)
		}
	})

	t.Run("credentialed redirect is refused", func(t *testing.T) {
		e := newEngine(withDefaults(Options{MaxFetches: 20}))
		e.client.Transport = &redirectRouteTransport{
			location: map[string]string{"https://a.test/graph.json": "https://user:pass@a.test/graph.json"},
		}
		if final, _, err := e.fetch(context.Background(), start, 1<<20); err == nil {
			t.Fatalf("redirect carrying credentials was allowed: final=%q", final)
		}
	})

	t.Run("redirect chain past the limit is refused", func(t *testing.T) {
		e := newEngine(withDefaults(Options{MaxFetches: 20}))
		// Every hop redirects again, so only the production CheckRedirect limit
		// (defaultMaxRedirects) can stop the chain.
		e.client.Transport = &redirectRouteTransport{
			location: map[string]string{
				"https://a.test/graph.json": "https://a.test/1",
				"https://a.test/1":          "https://a.test/2",
				"https://a.test/2":          "https://a.test/3",
				"https://a.test/3":          "https://a.test/4",
				"https://a.test/4":          "https://a.test/5",
				"https://a.test/5":          "https://a.test/6",
				"https://a.test/6":          "https://a.test/7",
			},
		}
		if final, _, err := e.fetch(context.Background(), start, 1<<20); err == nil {
			t.Fatalf("redirect chain past the limit was allowed: final=%q", final)
		}
	})
}

// A direct llms.txt entrypoint that advertises its graph by a relative link is
// discovered, while sibling links that are not graph publications are ignored.
func TestDiscoverFollowsDirectLLMSEntrypointWithRelativeManifest(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n<a href=\"notes.md\">notes</a>\n<a href=\"graph.json\">publication</a>\n")
	graph := publicationGraph(root, llms, nil)
	client := &http.Client{Transport: fixtureTransport{
		"https://docs.test/docs/llms.txt":   llms,
		"https://docs.test/docs/graph.json": graph,
		"https://docs.test/docs/index.md":   root,
	}}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://docs.test/docs/llms.txt", From: "/catalogue.md", Line: 8, Origin: "corpus"}}, Options{MaxFetches: 20}, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].URL != "https://docs.test/docs/graph.json" {
		t.Fatalf("relative manifest advertisement not followed: refs=%#v diagnostics=%#v", refs, diagnostics)
	}
	if refs[0].SHA256 != digest(graph) || refs[0].Root != "/index.md" || refs[0].From != "/catalogue.md" || refs[0].Line != 8 || refs[0].Origin != "corpus" {
		t.Fatalf("entrypoint provenance or identity lost: %#v", refs[0])
	}
}

// An interrupted direct llms.txt response is diagnosed without discarding an
// independent valid publication reached by another seed.
func TestDiscoverInterruptedLLMSEntrypointKeepsIndependentPublication(t *testing.T) {
	const interruptedURL = "https://broken.test/llms.txt"
	const validGraphURL = "https://valid.test/graph.json"
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	graph := publicationGraph(root, llms, nil)
	client := &http.Client{Transport: interruptedTransport{
		fixtureTransport: fixtureTransport{
			validGraphURL:                 graph,
			"https://valid.test/llms.txt": llms,
			"https://valid.test/index.md": root,
		},
		url: interruptedURL,
	}}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{
		{URL: interruptedURL, From: "/broken.md", Line: 1, Origin: "corpus"},
		{URL: validGraphURL, From: "/valid.md", Line: 2, Origin: "corpus"},
	}, Options{}, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].URL != validGraphURL {
		t.Fatalf("interrupted entrypoint changed valid results: refs=%#v diagnostics=%#v", refs, diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.URL == interruptedURL {
			return
		}
	}
	t.Fatalf("interrupted entrypoint was not diagnosed: %#v", diagnostics)
}

// validateURL refuses fragments, out-of-range ports, and hostless URLs while
// accepting a valid explicit port; a graph reference that is not canonical
// (here a query-bearing URL) is never admitted as a publication.
func TestValidateURLAndCanonicalGraphGuards(t *testing.T) {
	for _, raw := range []string{
		"https://h.test/a#frag",
		"https://h.test:0/a",
		"https://h.test:99999/a",
		"https:///nohost.md",
	} {
		if _, err := validateURL(raw); err == nil {
			t.Errorf("validateURL accepted unsafe %q", raw)
		}
	}
	if _, err := validateURL("https://h.test:8443/docs/"); err != nil {
		t.Errorf("validateURL rejected a valid custom port: %v", err)
	}

	graph := publicationGraph([]byte("# Root\n"), []byte("# Docs\n"), nil)
	client := &http.Client{Transport: fixtureTransport{
		"https://corp.test/graph.json?x=1": graph,
		"https://corp.test/llms.txt":       []byte("# Docs\n"),
		"https://corp.test/index.md":       []byte("# Root\n"),
	}}
	refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/graph.json?x=1", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{}, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 || len(diagnostics) == 0 {
		t.Fatalf("query-bearing graph reference admitted: refs=%#v diagnostics=%#v", refs, diagnostics)
	}
}

// A cached response is still subject to the smaller limit requested by a
// later consumer, without issuing another transport request.
func TestFetchCacheRespectsRequestedBodyLimit(t *testing.T) {
	const resource = "https://corp.test/page.md"
	transport := &countingTransport{
		responses: map[string][]byte{resource: []byte("0123456789")},
		calls:     map[string]int{},
	}
	e := newEngine(withDefaults(Options{MaxFetches: 4}))
	e.client.Transport = transport
	raw, err := validateURL(resource)
	if err != nil {
		t.Fatal(err)
	}
	if _, body, err := e.fetch(context.Background(), raw, 32); err != nil || string(body) != "0123456789" {
		t.Fatalf("initial fetch = body %q, err %v", body, err)
	}
	if _, _, err := e.fetch(context.Background(), raw, 4); err == nil {
		t.Fatal("cached response exceeded the requested limit without an error")
	}
	if transport.calls[resource] != 1 {
		t.Fatalf("cached response triggered another request: calls=%d", transport.calls[resource])
	}
}

func TestDiscoverRejectsLLMSByteDeclarationLimits(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	for _, declared := range []int{0, maxGraphLLMSBytes + 1} {
		t.Run(fmt.Sprintf("declared %d bytes", declared), func(t *testing.T) {
			manifest := validManifest(root, llms)
			manifest.LLMS.Bytes = declared
			client := &http.Client{Transport: fixtureTransport{
				"https://corp.test/graph.json": manifestBytes(manifest),
				"https://corp.test/llms.txt":   llms,
				"https://corp.test/index.md":   root,
			}}
			refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/graph.json", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{}, client)
			if err != nil {
				t.Fatal(err)
			}
			if len(refs) != 0 || len(diagnostics) == 0 {
				t.Fatalf("invalid llms byte declaration admitted: refs=%#v diagnostics=%#v", refs, diagnostics)
			}
		})
	}
}

// A publication whose required resources cannot be validated must be dropped
// with a diagnostic while an independent, fully valid publication reached in
// the same run is still returned. The pinned child decodes and matches its
// hash, so each failure is isolated to a missing resource declaration or an
// unavailable required resource.
func TestDiscoverRetainsValidPublicationWhenSiblingResourceFails(t *testing.T) {
	parentRoot := []byte("# Parent\n")
	parentLLMS := []byte("# Parent docs\n")
	childRoot := []byte("# Child\n")
	childLLMS := []byte("# Child docs\n")
	childURL := "https://corp.test/child/graph.json"

	cases := []struct {
		name      string
		childBody func() []byte
		fixtures  func(fixtureTransport)
	}{
		{
			name: "declared root absent from files",
			childBody: func() []byte {
				manifest := validManifest(childRoot, childLLMS)
				manifest.Files = map[string]graphFile{"/other.md": {Bytes: len(childRoot), SHA256: digest(childRoot)}}
				return manifestBytes(manifest)
			},
			fixtures: func(f fixtureTransport) {
				f["https://corp.test/child/llms.txt"] = childLLMS
				f["https://corp.test/child/index.md"] = childRoot
			},
		},
		{
			name:      "required llms resource is unavailable",
			childBody: func() []byte { return publicationGraph(childRoot, childLLMS, nil) },
			fixtures: func(f fixtureTransport) {
				// child/llms.txt intentionally omitted so its fetch returns 404.
				f["https://corp.test/child/index.md"] = childRoot
			},
		},
		{
			name:      "required root resource is unavailable",
			childBody: func() []byte { return publicationGraph(childRoot, childLLMS, nil) },
			fixtures: func(f fixtureTransport) {
				f["https://corp.test/child/llms.txt"] = childLLMS
				// child/index.md intentionally omitted so its fetch returns 404.
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			child := testCase.childBody()
			parent := publicationGraph(parentRoot, parentLLMS, []childReference{{
				URL: childURL, SHA256: digest(child), Root: "/index.md", From: "/index.md", Line: 1, Origin: "corpus",
			}})
			fixtures := fixtureTransport{
				"https://corp.test/parent/graph.json": parent,
				"https://corp.test/parent/llms.txt":   parentLLMS,
				"https://corp.test/parent/index.md":   parentRoot,
				"https://corp.test/child/graph.json":  child,
			}
			testCase.fixtures(fixtures)
			refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://corp.test/parent/graph.json", From: "src/catalogue.ts", Line: 2, Origin: "source"}}, Options{}, &http.Client{Transport: fixtures})
			if err != nil {
				t.Fatal(err)
			}
			parentKept := false
			for _, reference := range refs {
				if reference.URL == childURL {
					t.Fatalf("invalid child publication retained: %#v", refs)
				}
				if reference.URL == "https://corp.test/parent/graph.json" {
					parentKept = true
				}
			}
			if !parentKept || len(refs) != 1 {
				t.Fatalf("independent valid publication not retained: refs=%#v", refs)
			}
			if !hasDiagnostic(diagnostics, childURL) {
				t.Fatalf("failed child resource not reported: %#v", diagnostics)
			}
		})
	}
}

// When the same publication is reachable from several local candidates, the
// returned reference must carry the deterministic earliest provenance by
// candidate ordering, regardless of the order the candidates are supplied.
func TestDiscoverAttributesSharedPublicationToEarliestProvenance(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	graph := publicationGraph(root, llms, nil)
	graphURL := "https://docs.test/pub/graph.json"
	fixtures := fixtureTransport{
		graphURL:                         graph,
		"https://docs.test/pub/llms.txt": llms,
		"https://docs.test/pub/index.md": root,
	}

	cases := []struct {
		name       string
		candidates []Candidate
		wantFrom   string
		wantLine   int
		wantOrigin string
	}{
		{
			name: "earliest line wins",
			candidates: []Candidate{
				{URL: graphURL, From: "/catalogue.md", Line: 9, Origin: "corpus"},
				{URL: graphURL, From: "/catalogue.md", Line: 3, Origin: "corpus"},
			},
			wantFrom: "/catalogue.md", wantLine: 3, wantOrigin: "corpus",
		},
		{
			name: "earliest declaring path wins",
			candidates: []Candidate{
				{URL: graphURL, From: "/z-catalogue.md", Line: 4, Origin: "corpus"},
				{URL: graphURL, From: "/a-catalogue.md", Line: 4, Origin: "corpus"},
			},
			wantFrom: "/a-catalogue.md", wantLine: 4, wantOrigin: "corpus",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs, diagnostics, err := discoverWithClient(context.Background(), testCase.candidates, Options{MaxFetches: 20}, &http.Client{Transport: fixtures})
			if err != nil {
				t.Fatal(err)
			}
			if len(refs) != 1 || refs[0].URL != graphURL {
				t.Fatalf("shared publication not collapsed to one reference: refs=%#v diagnostics=%#v", refs, diagnostics)
			}
			if refs[0].From != testCase.wantFrom || refs[0].Line != testCase.wantLine || refs[0].Origin != testCase.wantOrigin {
				t.Fatalf("provenance = from=%q line=%d origin=%q, want from=%q line=%d origin=%q", refs[0].From, refs[0].Line, refs[0].Origin, testCase.wantFrom, testCase.wantLine, testCase.wantOrigin)
			}
		})
	}
}

func TestDiscoverSkipsPublishedSubtreesWithoutHidingSiblingPublications(t *testing.T) {
	root := []byte("# Root\n")
	llms := []byte("# Docs\n")
	graph := publicationGraph(root, llms, nil)
	transport := &countingTransport{
		responses: map[string][]byte{
			"https://alpha.test/graph.json":       graph,
			"https://alpha.test/llms.txt":         llms,
			"https://alpha.test/index.md":         root,
			"https://alpha.test/deep/guide.md":    []byte("# Already published\n"),
			"https://beta.test/docs/graph.json":   graph,
			"https://beta.test/docs/llms.txt":     llms,
			"https://beta.test/docs/index.md":     root,
			"https://beta.test/docs/ignored.html": []byte("<p>Already published</p>"),
			"https://beta.test/other/":            []byte(`<a href="graph.json">Documentation</a>`),
			"https://beta.test/other/graph.json":  graph,
			"https://beta.test/other/llms.txt":    llms,
			"https://beta.test/other/index.md":    root,
		},
		calls: map[string]int{},
	}
	candidates := []Candidate{
		{URL: "https://alpha.test/graph.json", From: "/a.md", Line: 1, Origin: "corpus"},
		{URL: "https://beta.test/docs/graph.json", From: "/b.md", Line: 1, Origin: "corpus"},
		{URL: "https://alpha.test/deep/guide.md", From: "/c.md", Line: 1, Origin: "corpus"},
		{URL: "https://beta.test/docs/ignored.html", From: "/d.md", Line: 1, Origin: "corpus"},
		{URL: "https://beta.test/other/", From: "/e.md", Line: 1, Origin: "corpus"},
	}
	refs, diagnostics, err := discoverWithClient(context.Background(), candidates, Options{MaxFetches: 10}, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, ref := range refs {
		found[ref.URL] = true
	}
	if len(refs) != 3 || !found["https://alpha.test/graph.json"] || !found["https://beta.test/docs/graph.json"] || !found["https://beta.test/other/graph.json"] {
		t.Fatalf("publication suppression hid a sibling or consumed its budget: refs=%#v diagnostics=%#v", refs, diagnostics)
	}
	for _, resource := range []string{"https://alpha.test/deep/guide.md", "https://beta.test/docs/ignored.html"} {
		if transport.calls[resource] != 0 {
			t.Fatalf("already-published resource was crawled: %s", resource)
		}
	}
}

func TestDiscoverRejectsExcessiveDeclarationsBeforeFetchingResources(t *testing.T) {
	root, llms := []byte("# Root\n"), []byte("# Docs\n")
	cases := []struct {
		name  string
		body  func() []byte
		valid bool
	}{
		{name: "valid control", valid: true, body: func() []byte { return publicationGraph(root, llms, nil) }},
		{name: "trailing invalid JSON", body: func() []byte {
			return append(publicationGraph(root, llms, nil), []byte(" trailing")...)
		}},
		{name: "too many files", body: func() []byte {
			m := validManifest(root, llms)
			file := graphFile{Bytes: 1, SHA256: digest([]byte("x"))}
			for i := range maxGraphFiles {
				m.Files[fmt.Sprintf("/extra-%d.md", i)] = file
			}
			return manifestBytes(m)
		}},
		{name: "aggregate corpus exceeds limit", body: func() []byte {
			m := validManifest(root, llms)
			file := graphFile{Bytes: maxGraphFileBytes, SHA256: digest([]byte("x"))}
			for i := range maxGraphCorpusBytes/maxGraphFileBytes + 1 {
				m.Files[fmt.Sprintf("/extra-%d.md", i)] = file
			}
			return manifestBytes(m)
		}},
		{name: "too many child references", body: func() []byte {
			m := validManifest(root, llms)
			hash := digest(root)
			for i := range maxGraphExternalReferences + 1 {
				m.ExternalReferences = append(m.ExternalReferences, childReference{
					URL:    fmt.Sprintf("https://children.test/%d/graph.json", i),
					SHA256: hash, Root: "/index.md", From: "/index.md", Line: 1, Origin: "corpus",
				})
			}
			return manifestBytes(m)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body()
			transport := &countingTransport{
				responses: map[string][]byte{
					"https://docs.test/graph.json": body,
					"https://docs.test/llms.txt":   llms,
					"https://docs.test/index.md":   root,
				},
				calls: map[string]int{},
			}
			refs, diagnostics, err := discoverWithClient(context.Background(), []Candidate{{URL: "https://docs.test/graph.json", From: "/index.md", Line: 1, Origin: "corpus"}}, Options{}, &http.Client{Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			if tc.valid {
				if len(refs) != 1 || refs[0].SHA256 != digest(body) {
					t.Fatalf("valid publication rejected: refs=%#v diagnostics=%#v", refs, diagnostics)
				}
				return
			}
			if len(refs) != 0 || !hasDiagnostic(diagnostics, "https://docs.test/graph.json") {
				t.Fatalf("invalid declaration admitted or not diagnosed: refs=%#v diagnostics=%#v", refs, diagnostics)
			}
			if transport.calls["https://docs.test/llms.txt"] != 0 || transport.calls["https://docs.test/index.md"] != 0 {
				t.Fatalf("resources fetched before rejecting invalid declarations: %#v", transport.calls)
			}
		})
	}
}

func TestDiscoverBoundsCredentialFloodWithoutLosingValidPublication(t *testing.T) {
	root, llms := []byte("# Root\n"), []byte("# Docs\n")
	transport := &countingTransport{
		responses: map[string][]byte{
			"https://docs.test/graph.json": publicationGraph(root, llms, nil),
			"https://docs.test/llms.txt":   llms,
			"https://docs.test/index.md":   root,
		},
		calls: map[string]int{},
	}
	candidates := []Candidate{{URL: "https://docs.test/graph.json", From: "/index.md", Line: 1, Origin: "corpus"}}
	for i := range maxDiagnostics + 1 {
		candidates = append(candidates, Candidate{URL: fmt.Sprintf("https://untrusted@bad.test/%d", i), From: "catalogue.json", Line: i + 1, Origin: "source"})
	}
	refs, diagnostics, err := discoverWithClient(context.Background(), candidates, Options{MaxFetches: 3}, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].URL != "https://docs.test/graph.json" || len(diagnostics) != maxDiagnostics || !hasDiagnostic(diagnostics, candidates[1].URL) {
		t.Fatalf("credential flood lost valid publication or diagnostic bound: refs=%#v diagnostics=%#v", refs, diagnostics)
	}
	for resource := range transport.calls {
		if strings.Contains(resource, "bad.test") {
			t.Fatalf("credential-bearing candidate reached the transport: %s", resource)
		}
	}
	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		stopped := &countingTransport{responses: transport.responses, calls: map[string]int{}}
		_, _, err := discoverWithClient(ctx, candidates, Options{}, &http.Client{Transport: stopped})
		if !errors.Is(err, context.Canceled) || len(stopped.calls) != 0 {
			t.Fatalf("cancellation did not stop requests: error=%v requests=%#v", err, stopped.calls)
		}
	})
}

func TestDiscoverReferenceBudgetSelectsCanonicalURLForSameSourceLine(t *testing.T) {
	root, llms := []byte("# Root\n"), []byte("# Docs\n")
	graph := publicationGraph(root, llms, nil)
	fixtures := fixtureTransport{}
	var candidates []Candidate
	for _, host := range []string{"z.test", "a.test"} {
		base := "https://" + host + "/"
		fixtures[base+"graph.json"], fixtures[base+"llms.txt"], fixtures[base+"index.md"] = graph, llms, root
		candidates = append(candidates, Candidate{URL: base + "graph.json", From: "catalogue.json", Line: 7, Origin: "source"})
	}
	refs, diagnostics, err := discoverWithClient(context.Background(), candidates, Options{MaxReferences: 1}, &http.Client{Transport: fixtures})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].URL != "https://a.test/graph.json" {
		t.Fatalf("bounded selection depended on candidate input order: refs=%#v diagnostics=%#v", refs, diagnostics)
	}
}
