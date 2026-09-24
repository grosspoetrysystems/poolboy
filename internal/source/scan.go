package source

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/grosspoetrysystems/poolboy/bundle"
)

// maxFileBytes bounds both hashing and heuristic inspection. A source file
// larger than this is outside the inventory boundary rather than partially
// represented by a misleading digest.
const maxFileBytes int64 = 5 << 20

// Aggregate caps keep a hostile or accidental tree from consuming unbounded
// memory/CPU even when every individual file is small.
const (
	maxInventoryFiles   = 10_000
	maxTraversedEntries = 100_000
)

// binaryProbeBytes keeps binary detection cheap while catching the usual NUL
// marker. The complete bounded file is still inspected for secret markers.
const binaryProbeBytes = 8 << 10

type scanner struct {
	root          string
	excludedDirs  map[string]bool
	excludedFiles map[string]bool
	entries       map[string]Entry
	traversed     int
}

// scanCurrent computes an inventory without reading or writing the baseline.
// Scan and Drift use this common path so Drift cannot accidentally refresh the
// lock while reporting evidence.
func scanCurrent(b *bundle.Bundle) (*Inventory, error) {
	root, err := projectRoot(b)
	if err != nil {
		return nil, err
	}
	dirs, files, err := buildExclusions(b, root)
	if err != nil {
		return nil, err
	}
	s := &scanner{
		root:          root,
		excludedDirs:  dirs,
		excludedFiles: files,
		entries:       make(map[string]Entry),
	}
	if err := s.walk(root, nil, nil); err != nil {
		return nil, err
	}
	return &Inventory{Version: Version, Files: s.entries}, nil
}

// walk descends only through safe, non-ignored directories. patterns are the
// higher-priority inherited rules; each directory's own files are appended so
// nested .gitignore/.poolboyignore rules have Git's expected precedence.
func (s *scanner) walk(dir string, inherited []gitignore.Pattern, rel []string) error {
	local, err := readIgnorePatterns(dir, s.root)
	if err != nil {
		return err
	}
	patterns := make([]gitignore.Pattern, 0, len(inherited)+len(local))
	patterns = append(patterns, inherited...)
	patterns = append(patterns, local...)

	entries, err := s.readEntries(dir, rel)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		childRel := appendPath(rel, name)
		child := filepath.Join(dir, name)
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		isDir := entry.IsDir()
		if !isDir {
			// Type may be unknown on some filesystems; Lstat gives us a
			// non-following check before any content read.
			info, statErr := os.Lstat(child)
			if statErr != nil {
				return fmt.Errorf("source: stat %s: %w", displayPath(childRel), statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			isDir = info.IsDir()
		}
		if isDir {
			if s.skipDir(childRel, name) || ignored(patterns, childRel, true) {
				continue
			}
			if err := s.walk(child, patterns, childRel); err != nil {
				return err
			}
			continue
		}
		if s.skipFile(childRel, name) || ignored(patterns, childRel, false) {
			continue
		}
		if err := s.addFile(child, childRel); err != nil {
			return err
		}
	}
	return nil
}

func (s *scanner) readEntries(dir string, rel []string) ([]os.DirEntry, error) {
	// #nosec G304 -- dir is rooted at validated projectRoot and reached only through non-symlink directory entries.
	file, err := os.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("source: reading %s: %w", displayPath(rel), err)
	}
	remaining := maxTraversedEntries - s.traversed
	entries, readErr := file.ReadDir(remaining + 1)
	closeErr := file.Close()
	if readErr != nil && readErr != io.EOF {
		return nil, fmt.Errorf("source: reading %s: %w", displayPath(rel), readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("source: closing %s: %w", displayPath(rel), closeErr)
	}
	if len(entries) > remaining {
		return nil, fmt.Errorf("source: traversal exceeds %d entries at %s", maxTraversedEntries, displayPath(rel))
	}
	s.traversed += len(entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	return entries, nil
}

func (s *scanner) skipDir(rel []string, name string) bool {
	if len(rel) == 0 {
		return false
	}
	path := strings.Join(rel, "/")
	if s.excludedDirs[path] || excludedSourceDirectory(name, len(rel) == 1) {
		return true
	}
	return false
}

func (s *scanner) skipFile(rel []string, name string) bool {
	path := strings.Join(rel, "/")
	if s.excludedFiles[path] || likelySecretName(name) {
		return true
	}
	for _, component := range rel[:len(rel)-1] {
		if likelySecretName(component) {
			return true
		}
	}
	return false
}

func (s *scanner) addFile(path string, rel []string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("source: stat %s: %w", displayPath(rel), err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil
	}
	if info.Size() < 0 || info.Size() > maxFileBytes {
		return nil
	}
	// #nosec G304 -- path was Lstat-validated as a regular non-symlink file under projectRoot.
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("source: opening %s: %w", displayPath(rel), err)
	}
	data, readErr := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	closeErr := f.Close()
	if readErr != nil {
		return fmt.Errorf("source: reading %s: %w", displayPath(rel), readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("source: closing %s: %w", displayPath(rel), closeErr)
	}
	if int64(len(data)) > maxFileBytes {
		return nil
	}
	if isBinary(data) || likelySecretContent(data) {
		return nil
	}
	pathKey := strings.Join(rel, "/")
	if len(s.entries) >= maxInventoryFiles {
		return fmt.Errorf("source: inventory exceeds %d files at %s", maxInventoryFiles, displayPath(rel))
	}
	digest := sha256.Sum256(data)
	s.entries[pathKey] = Entry{
		Type:   sourceType(pathKey),
		Bytes:  int64(len(data)),
		SHA256: fmt.Sprintf("%x", digest[:]),
	}
	return nil
}

func ignored(patterns []gitignore.Pattern, rel []string, isDir bool) bool {
	if len(patterns) == 0 {
		return false
	}
	return gitignore.NewMatcher(patterns).Match(rel, isDir)
}

func appendPath(base []string, name string) []string {
	out := make([]string, len(base)+1)
	copy(out, base)
	out[len(base)] = name
	return out
}

func displayPath(rel []string) string {
	if len(rel) == 0 {
		return "."
	}
	return strings.Join(rel, "/")
}

// buildExclusions derives project-relative corpus/config/output exclusions.
// Templates and data are excluded as authoring inputs; render outputs are
// excluded whether they are inside the corpus or another project subtree.
func buildExclusions(b *bundle.Bundle, root string) (map[string]bool, map[string]bool, error) {
	dirs := make(map[string]bool)
	files := make(map[string]bool)
	addDir := func(raw string) error {
		rel, err := projectRelative(root, raw, false)
		if err != nil {
			return err
		}
		if rel != "." {
			dirs[rel] = true
		}
		return nil
	}
	addFile := func(raw string) error {
		rel, err := projectRelative(root, raw, false)
		if err != nil {
			return err
		}
		if rel != "." {
			files[rel] = true
		}
		return nil
	}

	if b.Output != "" {
		if err := addDir(b.Output); err != nil {
			return nil, nil, fmt.Errorf("source: output path: %w", err)
		}
	}
	if b.Dir != "" {
		corpusRel, err := projectRelative(root, b.Dir, true)
		if err != nil {
			return nil, nil, fmt.Errorf("source: corpus path: %w", err)
		}
		if corpusRel != "." {
			dirs[corpusRel] = true
		}
	}
	corpus := root
	if b.Dir != "" {
		corpus = b.Dir
	}
	for _, render := range b.Renders {
		if render.Template != "" {
			if err := addFile(render.Template); err != nil {
				return nil, nil, fmt.Errorf("source: render template: %w", err)
			}
		}
		if render.Data != "" {
			if err := addFile(render.Data); err != nil {
				return nil, nil, fmt.Errorf("source: render data: %w", err)
			}
		}
		if render.Output != "" {
			if filepath.IsAbs(render.Output) {
				return nil, nil, fmt.Errorf("source: render output must be corpus-relative: %q", render.Output)
			}
			output := filepath.Join(corpus, filepath.FromSlash(render.Output))
			if err := addFile(output); err != nil {
				return nil, nil, fmt.Errorf("source: render output: %w", err)
			}
		}
	}
	if b.Landing.Logo != nil && strings.TrimSpace(*b.Landing.Logo) != "" {
		if err := addFile(*b.Landing.Logo); err != nil {
			return nil, nil, fmt.Errorf("source: landing logo: %w", err)
		}
	}
	if b.Landing.SiteDir != nil && strings.TrimSpace(*b.Landing.SiteDir) != "" {
		if err := addDir(*b.Landing.SiteDir); err != nil {
			return nil, nil, fmt.Errorf("source: landing site_dir: %w", err)
		}
	}

	return dirs, files, nil
}

// projectRelative converts an absolute or project-relative path to a clean,
// slash-separated project-relative path. allowDot permits the project root as
// a valid corpus value; all other traversal and absolute escapes fail closed.
func projectRelative(root, raw string, allowDot bool) (string, error) {
	if raw == "" {
		return ".", nil
	}
	path := raw
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes project root: %q", raw)
	}
	if rel == "." && !allowDot {
		return "", fmt.Errorf("path names project root: %q", raw)
	}
	if rel == "." {
		return rel, nil
	}
	return filepath.ToSlash(rel), nil
}

func readIgnorePatterns(dir, root string) ([]gitignore.Pattern, error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("source: ignore directory escapes project root: %s", dir)
	}
	var domain []string
	if rel != "." {
		domain = strings.Split(filepath.ToSlash(rel), "/")
	}
	var patterns []gitignore.Pattern
	for _, name := range []string{".gitignore", ".poolboyignore"} {
		path := filepath.Join(dir, name)
		info, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return nil, fmt.Errorf("source: checking %s: %w", displayPath(append(domain, name)), statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("source: ignore file %s is not a regular file", displayPath(append(domain, name)))
		}
		// #nosec G304 -- path was Lstat-validated as a regular non-symlink ignore file under projectRoot.
		f, openErr := os.Open(path)
		if openErr != nil {
			return nil, fmt.Errorf("source: opening %s: %w", displayPath(append(domain, name)), openErr)
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4<<10), 1<<20)
		for scanner.Scan() {
			line := strings.TrimSuffix(scanner.Text(), "\r")
			if line == "" || strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
				continue
			}
			patterns = append(patterns, gitignore.ParsePattern(normalizeTrailingGlob(line), domain))
		}
		scanErr := scanner.Err()
		closeErr := f.Close()
		if scanErr != nil {
			return nil, fmt.Errorf("source: reading %s: %w", displayPath(append(domain, name)), scanErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("source: closing %s: %w", displayPath(append(domain, name)), closeErr)
		}
	}
	return patterns, nil
}

// normalizeTrailingGlob compensates for go-git's matcher treating a trailing
// /** pattern as matching the directory named before it. Git treats that
// directory as traversable while ignoring its contents, so make the final
// segment explicit without changing the files selected by the pattern.
func normalizeTrailingGlob(line string) string {
	negation := ""
	pattern := line
	if strings.HasPrefix(pattern, "!") {
		negation, pattern = "!", pattern[1:]
	}
	if strings.HasSuffix(pattern, "/**") {
		return negation + pattern + "/*"
	}
	return line
}

var dependencyDirNames = map[string]bool{
	".git":          true,
	".substrate":    true,
	".poolboy":      true,
	"node_modules":  true,
	"vendor":        true,
	".cache":        true,
	".next":         true,
	".venv":         true,
	"venv":          true,
	"__pycache__":   true,
	".pytest_cache": true,
	".mypy_cache":   true,
	".ruff_cache":   true,
	".tox":          true,
}

var rootBuildDirNames = map[string]bool{
	"target":   true,
	"build":    true,
	"dist":     true,
	"out":      true,
	"bin":      true,
	"obj":      true,
	"coverage": true,
}

func excludedSourceDirectory(name string, root bool) bool {
	return dependencyDirNames[name] || (root && rootBuildDirNames[name])
}

func excludedDirectoryName(name string) bool {
	return dependencyDirNames[name] || rootBuildDirNames[name]
}

var sourceTypeByExt = map[string]string{
	".go": "go", ".rs": "rust", ".py": "python", ".rb": "ruby", ".java": "java",
	".kt": "kotlin", ".swift": "swift", ".c": "c", ".h": "c", ".cc": "cpp",
	".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".cs": "csharp", ".php": "php",
	".ts": "typescript", ".tsx": "typescript", ".js": "javascript", ".jsx": "javascript",
	".mjs": "javascript", ".cjs": "javascript", ".vue": "vue", ".svelte": "svelte",
	".sh": "shell", ".bash": "shell", ".zsh": "shell", ".fish": "shell", ".sql": "sql",
	".md": "markdown", ".markdown": "markdown", ".json": "json", ".yaml": "yaml",
	".yml": "yaml", ".toml": "toml", ".xml": "xml", ".html": "html", ".htm": "html",
	".css": "css", ".scss": "scss", ".proto": "protobuf", ".graphql": "graphql",
}

func sourceType(path string) string {
	base := filepath.Base(path)
	switch base {
	case "Dockerfile", "Containerfile":
		return "dockerfile"
	case "Makefile", "GNUmakefile":
		return "make"
	case ".gitignore", ".poolboyignore":
		return "ignore"
	}
	if typ := sourceTypeByExt[strings.ToLower(filepath.Ext(base))]; typ != "" {
		return typ
	}
	return "other"
}

func isBinary(data []byte) bool {
	limit := len(data)
	if limit > binaryProbeBytes {
		limit = binaryProbeBytes
	}
	return bytes.IndexByte(data[:limit], 0) >= 0
}
