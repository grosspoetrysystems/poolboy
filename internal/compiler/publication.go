package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const maxPublicationManifestBytes = 32 << 20

// validateExistingPublication permits replacing only an empty directory or a
// complete publication produced by Poolboy. Unknown files are never silently
// removed by the directory swap.
func validateExistingPublication(output string) error {
	fi, err := os.Lstat(output)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect publication output: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("publication output must not be a symlink: %s", output)
	}
	if !fi.IsDir() {
		return fmt.Errorf("publication output is not a directory: %s", output)
	}
	// #nosec G304 -- output is normalized and contained by validateRoots before this call.
	dir, err := os.Open(output)
	if err != nil {
		return fmt.Errorf("read publication output: %w", err)
	}
	names, err := dir.Readdirnames(1)
	_ = dir.Close()
	if err != nil {
		return fmt.Errorf("read publication output: %w", err)
	}
	if len(names) == 0 {
		return nil
	}
	graphPath := filepath.Join(output, "graph.json")
	graphData, err := readBoundedRegular(graphPath, maxPublicationManifestBytes, "publication graph")
	if err != nil {
		return fmt.Errorf("existing publication is not Poolboy-owned: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(graphData))
	var manifest graph
	if err := dec.Decode(&manifest); err != nil {
		return fmt.Errorf("existing publication has invalid graph.json: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("existing publication has multiple graph.json values")
		}
		return fmt.Errorf("existing publication has trailing graph.json data: %w", err)
	}
	if manifest.Version != "0" || manifest.Root != "/index.md" || len(manifest.Files) == 0 {
		return fmt.Errorf("existing publication has incompatible graph.json")
	}
	if len(manifest.Files) > maxCorpusDocuments {
		return fmt.Errorf("existing publication graph exceeds %d Markdown files", maxCorpusDocuments)
	}
	var declaredBytes int64
	for _, file := range manifest.Files {
		if file.Bytes < 0 || file.Bytes > maxMarkdownBytes {
			return fmt.Errorf("existing publication graph contains an invalid Markdown size")
		}
		declaredBytes += int64(file.Bytes)
		if declaredBytes > maxCorpusBytes {
			return fmt.Errorf("existing publication graph exceeds %d bytes", maxCorpusBytes)
		}
	}
	if len(manifest.Artifacts) > maxLandingFiles {
		return fmt.Errorf("existing publication graph exceeds %d artifacts", maxLandingFiles)
	}
	var declaredArtifactBytes int64
	for path, file := range manifest.Artifacts {
		rel, err := publicationArtifactPath(path)
		if err != nil {
			return fmt.Errorf("existing publication artifact: %w", err)
		}
		limit := int64(maxOutputBytes)
		if rel == "corpus.zip" {
			limit = maxCorpusBytes
		}
		if file.Bytes < 0 || int64(file.Bytes) > limit || len(file.SHA256) != sha256.Size*2 || strings.ToLower(file.SHA256) != file.SHA256 {
			return fmt.Errorf("existing publication has invalid artifact entry: %s", path)
		}
		if _, err := hex.DecodeString(file.SHA256); err != nil {
			return fmt.Errorf("existing publication has invalid artifact hash for %s: %w", path, err)
		}
		declaredArtifactBytes += int64(file.Bytes)
		if declaredArtifactBytes > maxLandingBytes {
			return fmt.Errorf("existing publication graph exceeds %d artifact bytes", maxLandingBytes)
		}
	}

	expected := map[string]bool{"graph.json": true, "llms.txt": true}
	expectedDirs := map[string]bool{}
	seenGraph := map[string]bool{}
	seenArtifact := map[string]bool{}
	seenSpecial := map[string]bool{}
	seenCanonical := map[string]string{}
	for graphPath, file := range manifest.Files {
		rel, err := publicationGraphPath(graphPath)
		if err != nil {
			return fmt.Errorf("existing publication graph: %w", err)
		}
		key := canonicalPathKey(graphPath)
		if prior, ok := seenCanonical[key]; ok && prior != graphPath {
			return fmt.Errorf("existing publication graph paths collide: %s and %s", prior, graphPath)
		}
		seenCanonical[key] = graphPath
		if file.Bytes < 0 || len(file.SHA256) != sha256.Size*2 {
			return fmt.Errorf("existing publication has invalid graph entry: %s", graphPath)
		}
		if _, err := hex.DecodeString(file.SHA256); err != nil {
			return fmt.Errorf("existing publication has invalid graph hash for %s: %w", graphPath, err)
		}
		expected[filepath.ToSlash(rel)] = true
		parts := strings.Split(filepath.ToSlash(rel), "/")
		for i := 1; i < len(parts); i++ {
			expectedDirs[strings.Join(parts[:i], "/")] = true
		}
	}
	for artifactPath := range manifest.Artifacts {
		rel, err := publicationArtifactPath(artifactPath)
		if err != nil {
			return fmt.Errorf("existing publication artifact: %w", err)
		}
		key := canonicalPathKey("/" + rel)
		if prior, ok := seenCanonical[key]; ok && prior != artifactPath {
			return fmt.Errorf("existing publication paths collide: %s and %s", prior, artifactPath)
		}
		seenCanonical[key] = artifactPath
		expected[rel] = true
		parts := strings.Split(rel, "/")
		for i := 1; i < len(parts); i++ {
			expectedDirs[strings.Join(parts[:i], "/")] = true
		}
	}

	indexEntry, ok := manifest.Files["/index.md"]
	if !ok || indexEntry.SHA256 == "" {
		return fmt.Errorf("existing publication graph is missing /index.md")
	}
	err = filepath.WalkDir(output, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("existing publication contains symlink: %s", path)
		}
		rel, err := filepath.Rel(output, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if !expectedDirs[rel] {
				return fmt.Errorf("existing publication contains unknown directory: %s", rel)
			}
			return nil
		}
		if !expected[rel] {
			return fmt.Errorf("existing publication contains unknown file: %s", rel)
		}
		if rel == "graph.json" || rel == "llms.txt" {
			limit := int64(maxPublicationManifestBytes)
			if rel == "llms.txt" {
				limit = maxOutputBytes
			}
			if _, err := readBoundedRegular(path, limit, "published metadata"); err != nil {
				return err
			}
			seenSpecial[rel] = true
			return nil
		}
		if entry, ok := manifest.Artifacts[rel]; ok {
			limit := int64(maxOutputBytes)
			if rel == "corpus.zip" {
				limit = maxCorpusBytes
			}
			data, err := readBoundedRegular(path, limit, "published artifact")
			if err != nil {
				return err
			}
			if len(data) != entry.Bytes || hashBytes(data) != entry.SHA256 {
				return fmt.Errorf("existing publication graph does not own %s", rel)
			}
			seenArtifact[rel] = true
			return nil
		}
		entry := manifest.Files["/"+rel]
		data, err := readBoundedRegular(path, maxMarkdownBytes, "published Markdown")
		if err != nil {
			return err
		}
		if len(data) != entry.Bytes || hashBytes(data) != entry.SHA256 {
			return fmt.Errorf("existing publication graph does not own %s", rel)
		}
		seenGraph["/"+rel] = true
		return nil
	})
	if err != nil {
		return err
	}
	if !seenSpecial["graph.json"] || !seenSpecial["llms.txt"] {
		return fmt.Errorf("existing publication is missing graph.json or llms.txt")
	}
	for graphPath := range manifest.Files {
		if !seenGraph[graphPath] {
			return fmt.Errorf("existing publication is missing %s", graphPath)
		}
	}
	for artifactPath := range manifest.Artifacts {
		if !seenArtifact[artifactPath] {
			return fmt.Errorf("existing publication is missing %s", artifactPath)
		}
	}
	return nil
}

func publicationGraphPath(path string) (string, error) {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || strings.Contains(path, "#") {
		return "", fmt.Errorf("invalid graph path %q", path)
	}
	rel, err := cleanRelative("publication graph path", strings.TrimPrefix(path, "/"))
	if err != nil || "/"+filepath.ToSlash(rel) != path || !strings.HasSuffix(strings.ToLower(rel), ".md") {
		return "", fmt.Errorf("invalid graph path %q", path)
	}
	return rel, nil
}

func publicationArtifactPath(path string) (string, error) {
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, `\#?%`) {
		return "", fmt.Errorf("invalid artifact path %q", path)
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("invalid artifact path %q", path)
		}
	}
	rel, err := cleanRelative("publication artifact path", path)
	lower := strings.ToLower(rel)
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("invalid artifact path %q", path)
		}
	}
	if err != nil || filepath.ToSlash(rel) != path || lower == "graph.json" || lower == "llms.txt" || strings.HasSuffix(lower, ".md") {
		return "", fmt.Errorf("invalid artifact path %q", path)
	}
	return rel, nil
}
