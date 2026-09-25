package compiler

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/index"
	"github.com/grosspoetrysystems/poolboy/parse"
	"gopkg.in/yaml.v3"
)

type graph struct {
	Version   string                   `json:"version"`
	Root      string                   `json:"root"`
	LLMS      graphArtifact            `json:"llms"`
	Files     map[string]graphFile     `json:"files"`
	Artifacts map[string]graphArtifact `json:"artifacts,omitempty"`
}

type graphArtifact struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type graphFile struct {
	Title   string           `json:"title,omitempty"`
	Bytes   int              `json:"bytes"`
	SHA256  string           `json:"sha256"`
	Links   []string         `json:"links"`
	Type    string           `json:"type,omitempty"`
	Sources []map[string]any `json:"sources,omitempty"`
}

var (
	referenceLink = regexp.MustCompile(`\]\s*\[[^\]]*\]`)
	referenceDef  = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]]+\]:\s*\S+`)
	htmlHref      = regexp.MustCompile(`(?i)<\s*a\b[^>]*\bhref\s*=`)
)

func buildGraph(idx *index.Index, docs map[string]document, artifacts map[string][]byte, llms []byte) ([]byte, error) {
	if _, ok := docs["/index.md"]; !ok {
		return nil, fmt.Errorf("corpus is missing required root /index.md")
	}
	if err := validateIndex(idx); err != nil {
		return nil, err
	}
	files := make(map[string]graphFile, len(docs))
	entries := append([]*index.Entry(nil), idx.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	for _, entry := range entries {
		doc, ok := docs[entry.Path]
		if !ok {
			return nil, fmt.Errorf("indexed document missing from candidate corpus: %s", entry.Path)
		}
		links := make([]string, 0, len(entry.Links))
		seen := map[string]bool{}
		for _, link := range entry.Links {
			if link.Target == "" || seen[link.Target] {
				continue
			}
			seen[link.Target] = true
			links = append(links, link.Target)
		}
		sort.Strings(links)
		file := graphFile{
			Title:  entry.Field("title"),
			Bytes:  len(doc.data),
			SHA256: hashBytes(doc.data),
			Links:  links,
			Type:   entry.Type,
		}
		file.Sources = sourcesFromMarkdown(doc.data)
		files[entry.Path] = file
	}
	if len(files) != len(docs) {
		return nil, fmt.Errorf("indexed/candidate document set mismatch: indexed %d, candidate %d", len(files), len(docs))
	}
	for path := range docs {
		if _, ok := files[path]; !ok {
			return nil, fmt.Errorf("candidate document missing from index: %s", path)
		}
	}
	artifactManifest := make(map[string]graphArtifact, len(artifacts))
	for path, data := range artifacts {
		if _, err := publicationArtifactPath(path); err != nil {
			return nil, err
		}
		if len(data) > maxOutputBytes && path != "corpus.zip" {
			return nil, fmt.Errorf("artifact exceeds %d bytes: %s", maxOutputBytes, path)
		}
		if path == "corpus.zip" && int64(len(data)) > maxCorpusBytes {
			return nil, fmt.Errorf("artifact exceeds %d bytes: %s", maxCorpusBytes, path)
		}
		artifactManifest[path] = graphArtifact{Bytes: len(data), SHA256: hashBytes(data)}
	}
	data, err := json.MarshalIndent(graph{Version: "0", Root: "/index.md", LLMS: graphArtifact{Bytes: len(llms), SHA256: hashBytes(llms)}, Files: files, Artifacts: artifactManifest}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode graph: %w", err)
	}
	if len(data)+1 > maxPublicationManifestBytes {
		return nil, fmt.Errorf("graph exceeds %d-byte limit", maxPublicationManifestBytes)
	}
	return append(data, '\n'), nil
}

func validateIndex(idx *index.Index) error {
	if idx == nil {
		return fmt.Errorf("build: missing document index")
	}
	for _, entry := range idx.Entries {
		body, err := entry.Body()
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Path, err)
		}
		if err := rejectUnsupportedLinks(entry.Path, body); err != nil {
			return err
		}
		set := parse.Links(body)
		if len(set.Absolute) != 0 {
			return fmt.Errorf("root-relative authored links are not portable: %s", entry.Path)
		}
		for _, link := range set.Relative {
			if err := validateRawInternalTarget(entry.Path, link.Target); err != nil {
				return err
			}
			target, outside := idx.ResolveLink(entry.Path, link.Target)
			if outside {
				return fmt.Errorf("link escapes corpus: %s -> %s", entry.Path, link.Target)
			}
			targetPath := target
			if hash := strings.IndexByte(targetPath, '#'); hash >= 0 {
				targetPath = targetPath[:hash]
			}
			if !idx.FileExists(targetPath) {
				return fmt.Errorf("broken link: %s -> %s", entry.Path, link.Target)
			}
		}
		for _, link := range set.SelfAnchors {
			if err := validateFragment(entry.Path, link.Target); err != nil {
				return err
			}
		}
		for _, link := range set.External {
			if err := validateExternalURL(entry.Path, link.Target); err != nil {
				return err
			}
		}
	}
	for _, issue := range idx.Check() {
		if issue.Level != "warning" {
			continue
		}
		if strings.Contains(issue.Msg, "broken link ->") ||
			strings.Contains(issue.Msg, "link anchor not found ->") ||
			strings.Contains(issue.Msg, "out-of-bundle link ->") {
			return fmt.Errorf("invalid link in %s: %s", issue.Entry, issue.Msg)
		}
	}
	return nil
}

func rejectUnsupportedLinks(path, body string) error {
	body = parse.WithoutCode(body)
	if referenceDef.MatchString(body) || hasReferenceLink(body) {
		return fmt.Errorf("unsupported reference-style Markdown link in %s", path)
	}
	if htmlHref.MatchString(body) {
		return fmt.Errorf("unsupported HTML href link in %s", path)
	}
	// Wikilinks are recognized by the imported index for maintenance, but they
	// are not portable Markdown and would otherwise be silently absent from the
	// standard-link graph.
	if strings.Contains(body, "[[") || strings.Contains(body, "]]") {
		return fmt.Errorf("unsupported wikilink construct in %s", path)
	}
	return nil
}

func hasReferenceLink(body string) bool {
	for _, match := range referenceLink.FindAllStringIndex(body, -1) {
		if match[1] < len(body) && body[match[1]] == '(' {
			continue
		}
		return true
	}
	return false
}

func validateRawInternalTarget(fromPath, target string) error {
	pathPart, _, _ := strings.Cut(target, "#")
	if pathPart == "" {
		return nil
	}
	if strings.Contains(pathPart, "\\") || strings.IndexByte(pathPart, 0) >= 0 {
		return fmt.Errorf("unsafe link target in %s: %q", fromPath, target)
	}
	decoded, err := url.PathUnescape(pathPart)
	if err != nil {
		return fmt.Errorf("invalid escaped link target in %s: %q", fromPath, target)
	}
	if strings.HasPrefix(decoded, "/") {
		return fmt.Errorf("root-relative authored link in %s: %q", fromPath, target)
	}
	depth := strings.Count(strings.Trim(strings.TrimSuffix(fromPath, "/"), "/"), "/")
	for _, part := range strings.Split(decoded, "/") {
		switch part {
		case "", ".":
		case "..":
			if depth == 0 {
				return fmt.Errorf("link traversal escapes corpus in %s: %q", fromPath, target)
			}
			depth--
		default:
			depth++
		}
	}
	return nil
}

func validateFragment(path, target string) error {
	_, fragment, _ := strings.Cut(target, "#")
	if fragment == "" {
		return nil
	}
	if !utf8.ValidString(fragment) || strings.ContainsAny(fragment, "\r\n") {
		return fmt.Errorf("invalid link fragment in %s: %q", path, target)
	}
	return nil
}

func validateExternalURL(path, target string) error {
	scheme := strings.ToLower(strings.TrimSuffix(strings.SplitN(target, ":", 2)[0], ":"))
	switch scheme {
	case "javascript", "data", "file", "vbscript", "about", "blob":
		return fmt.Errorf("executable or local URL scheme %q is forbidden in %s", scheme, path)
	}
	return nil
}

func sourcesFromMarkdown(data []byte) []map[string]any {
	block := frontmatterBlock(string(data))
	if block == "" {
		return nil
	}
	var root map[string]any
	if err := yaml.Unmarshal([]byte(block), &root); err != nil {
		return nil
	}
	raw, ok := root["sources"]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		clean := map[string]any{}
		for key, value := range item {
			switch value.(type) {
			case string, bool, int, int64, float64, []any, map[string]any:
				clean[key] = value
			}
		}
		if len(clean) > 0 {
			out = append(out, clean)
		}
	}
	return out
}

func frontmatterBlock(content string) string {
	content = strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	lineEnd := strings.IndexByte(content, '\n')
	if lineEnd < 0 {
		return ""
	}
	rest := content[lineEnd+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func renderLLMS(name string) []byte {
	name = strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(name))
	if name == "" {
		name = "Poolboy corpus"
	}
	return []byte(fmt.Sprintf("# %s\n\nAgent-native documentation corpus for %s.\n\n- [Start](index.md)\n- [Manifest](graph.json)\n", name, name))
}

func stageIndex(b *bundle.Bundle, stage string) (*index.Index, error) {
	copyBundle := *b
	copyBundle.Dir = stage
	return index.Build(&copyBundle)
}
