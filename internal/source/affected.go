package source

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/parse"
)

// Affected returns canonical corpus document IDs whose OKF sources metadata
// names source exactly. Project-root resources (for example src/routes.go) are
// resolved from the project root; explicit ./ and ../ resources are resolved
// from the declaring document's directory. Results are review candidates, not
// semantic staleness claims.
func Affected(b *bundle.Bundle, source string) ([]string, error) {
	root, err := projectRoot(b)
	if err != nil {
		return nil, err
	}
	corpus := root
	if b.Dir != "" {
		corpus = filepath.Clean(b.Dir)
		if !filepath.IsAbs(corpus) {
			corpus = filepath.Join(root, filepath.FromSlash(corpus))
		}
	}
	if _, err := projectRelative(root, corpus, true); err != nil {
		return nil, fmt.Errorf("source: corpus path: %w", err)
	}
	corpusInfo, err := os.Lstat(corpus)
	if err != nil {
		return nil, fmt.Errorf("source: corpus: %w", err)
	}
	if corpusInfo.Mode()&os.ModeSymlink != 0 || !corpusInfo.IsDir() {
		return nil, fmt.Errorf("source: corpus is not a real directory: %s", corpus)
	}
	target, err := canonicalInput(root, source)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var walk func(string) error
	walk = func(dir string) error {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return fmt.Errorf("source: reading corpus: %w", readErr)
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if excludedDirectoryName(entry.Name()) {
					continue
				}
				if err := walk(path); err != nil {
					return err
				}
				continue
			}
			if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				continue
			}
			info, statErr := os.Lstat(path)
			if statErr != nil {
				return fmt.Errorf("source: stat corpus document: %w", statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				continue
			}
			// #nosec G304 -- path is within validated corpus and Lstat-validated as a regular non-symlink file.
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("source: reading corpus document: %w", readErr)
			}
			resources := frontmatterResources(string(content))
			for _, resource := range resources {
				for _, candidate := range resourceCandidates(root, path, resource) {
					if candidate == target {
						rel, relErr := filepath.Rel(corpus, path)
						if relErr != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
							return fmt.Errorf("source: corpus document escapes corpus: %s", path)
						}
						seen["/"+filepath.ToSlash(rel)] = true
						break
					}
				}
			}
		}
		return nil
	}
	if err := walk(corpus); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(seen))
	for doc := range seen {
		result = append(result, doc)
	}
	sort.Strings(result)
	return result, nil
}

func canonicalInput(root, raw string) (string, error) {
	raw = bundle.NormalizeResource(raw)
	if raw == "" {
		return "", fmt.Errorf("source: source path is required")
	}
	if hasURLScheme(raw) {
		return "", fmt.Errorf("source: source must be a project path, not a URL")
	}
	path, err := bundle.ResolveResourcePath(root, root, raw)
	if err != nil {
		return "", fmt.Errorf("source: invalid source path: %w", err)
	}
	return projectRelative(root, path, false)
}

func resourceCandidates(root, document, raw string) []string {
	raw = bundle.NormalizeResource(raw)
	if raw == "" || hasURLScheme(raw) {
		return nil
	}
	path, err := bundle.ResolveResourcePath(root, document, raw)
	if err != nil {
		return nil
	}
	canonical, err := projectRelative(root, path, false)
	if err != nil {
		return nil
	}
	return []string{canonical}
}

func hasURLScheme(value string) bool {
	for i, c := range value {
		if c == ':' {
			if i == 0 {
				return false
			}
			for j, r := range value[:i] {
				letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
				digit := j > 0 && r >= '0' && r <= '9'
				punctuation := r == '+' || r == '-' || r == '.'
				if letter || digit || punctuation {
					continue
				}
				return false
			}
			return true
		}
		if c == '/' || c == '\\' {
			return false
		}
	}
	return false
}

// frontmatterResources extracts only sources[].resource from the parsed OKF
// frontmatter. The shared parser preserves nested YAML maps and sequences, so
// provenance does not need a second YAML grammar in this package.
func frontmatterResources(content string) []string {
	fm, _ := parse.Frontmatter(content)
	raw, ok := fm["sources"]
	if !ok {
		return nil
	}
	var resources []string
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			if resource, ok := value["resource"].(string); ok {
				if resource = bundle.NormalizeResource(resource); resource != "" {
					resources = append(resources, resource)
				}
			}
		case []any:
			for _, item := range value {
				visit(item)
			}
		case []string:
			for _, item := range value {
				if item = bundle.NormalizeResource(item); item != "" {
					resources = append(resources, item)
				}
			}
		}
	}
	visit(raw)
	return resources
}
