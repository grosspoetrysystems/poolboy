package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/index"
)

const (
	maxMarkdownBytes   = 8 << 20
	maxTemplateBytes   = 2 << 20
	maxDataBytes       = 8 << 20
	maxOutputBytes     = 4 << 20
	maxOutputUTF16     = 100_000
	maxCorpusBytes     = 64 << 20
	maxCorpusDocuments = 10_000
)

type roots struct {
	project string
	corpus  string
	output  string
	bundle  *bundle.Bundle
}

type document struct {
	path string // canonical root-absolute graph path
	data []byte // normalized UTF-8 Markdown bytes
}

func validateRoots(b *bundle.Bundle) (roots, error) {
	if b == nil {
		return roots{}, fmt.Errorf("build: missing bundle")
	}
	if b.Root == "" {
		return roots{}, fmt.Errorf("build: missing project root")
	}
	project, err := filepath.Abs(b.Root)
	if err != nil {
		return roots{}, fmt.Errorf("project root: %w", err)
	}
	project = filepath.Clean(project)
	if err := existingDir(project, "project root"); err != nil {
		return roots{}, err
	}
	corpus, err := checkedWithin(project, b.Dir, "corpus")
	if err != nil {
		return roots{}, err
	}
	if err := existingDir(corpus, "corpus"); err != nil {
		return roots{}, err
	}
	if b.Output == "" {
		return roots{}, fmt.Errorf("build: missing publication output")
	}
	if filepath.IsAbs(b.Output) || filepath.VolumeName(b.Output) != "" {
		return roots{}, fmt.Errorf("output must be project-relative: %q", b.Output)
	}
	if _, err := cleanRelative("output", b.Output); err != nil {
		return roots{}, err
	}
	output := filepath.Join(project, filepath.FromSlash(filepath.ToSlash(b.Output)))
	if reservedProjectPath(project, output) {
		return roots{}, fmt.Errorf("publication output targets a reserved project directory: %s", output)
	}
	if err := noSymlinkPath(project, output); err != nil {
		return roots{}, err
	}
	if fi, err := os.Lstat(output); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return roots{}, fmt.Errorf("publication output must not be a symlink: %s", output)
		}
		if !fi.IsDir() {
			return roots{}, fmt.Errorf("publication output is not a directory: %s", output)
		}
	} else if !os.IsNotExist(err) {
		return roots{}, fmt.Errorf("publication output: %w", err)
	}
	if sameOrWithin(output, corpus) {
		return roots{}, fmt.Errorf("publication output contains corpus: %s and %s", output, corpus)
	}
	return roots{project: project, corpus: corpus, output: output, bundle: b}, nil
}

func validateMappings(b *bundle.Bundle, r roots) error {
	seen := map[string]string{}
	for i, mapping := range b.Renders {
		if mapping.Template == "" || mapping.Data == "" || mapping.Output == "" {
			return fmt.Errorf("render[%d] requires template, data and output", i)
		}
		template, err := checkedRelative(fmt.Sprintf("render[%d].template", i), mapping.Template)
		if err != nil {
			return err
		}
		data, err := checkedRelative(fmt.Sprintf("render[%d].data", i), mapping.Data)
		if err != nil {
			return err
		}
		output, err := cleanRelative(fmt.Sprintf("render[%d].output", i), mapping.Output)
		if err != nil {
			return err
		}
		if hiddenDirectory(output) {
			return fmt.Errorf("render[%d].output cannot target a hidden directory: %q", i, output)
		}
		if !strings.HasSuffix(strings.ToLower(output), ".md") {
			return fmt.Errorf("render[%d].output must end in .md: %q", i, mapping.Output)
		}
		templatePath := filepath.Join(r.project, filepath.FromSlash(template))
		if err := rejectPathCollision(templatePath, r.output); err != nil {
			return fmt.Errorf("render[%d].template: %w", i, err)
		}
		dataPath := filepath.Join(r.project, filepath.FromSlash(data))
		if err := rejectPathCollision(dataPath, r.output); err != nil {
			return fmt.Errorf("render[%d].data: %w", i, err)
		}
		if _, err := readBoundedRegular(templatePath, maxTemplateBytes, "render template"); err != nil {
			return err
		}
		if _, err := readBoundedRegular(dataPath, maxDataBytes, "render data"); err != nil {
			return err
		}
		key := canonicalPathKey("/" + filepath.ToSlash(output))
		if prior, ok := seen[key]; ok {
			return fmt.Errorf("render outputs collide: %q and %q", prior, output)
		}
		seen[key] = output
		outPath := filepath.Join(r.corpus, filepath.FromSlash(output))
		if sameOrWithin(r.output, outPath) || sameOrWithin(outPath, r.output) {
			return fmt.Errorf("render[%d].output overlaps publication output: %s", i, outPath)
		}
		if index.MatchIgnore(b, outPath) {
			return fmt.Errorf("render[%d].output matches an ignored corpus path: %s", i, outPath)
		}
		if err := noSymlinkPath(r.corpus, outPath); err != nil {
			return fmt.Errorf("render[%d].output: %w", i, err)
		}
		if fi, err := os.Lstat(outPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("render[%d].output must not be a symlink: %s", i, outPath)
		}
	}
	return nil
}
func collectCorpus(r roots) (map[string]document, error) {
	docs := map[string]document{}
	seen := map[string]string{}
	var totalBytes int64
	err := filepath.WalkDir(r.corpus, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("corpus contains symlink: %s", path)
		}
		if d.IsDir() && filepath.Clean(path) == filepath.Clean(r.output) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if path != r.corpus && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if index.MatchIgnore(r.bundle, path) {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(r.corpus, path)
		if err != nil {
			return err
		}
		graphPath := "/" + filepath.ToSlash(rel)
		if _, ok := seen[canonicalPathKey(graphPath)]; ok {
			return fmt.Errorf("corpus path collision: %s", graphPath)
		}
		seen[canonicalPathKey(graphPath)] = graphPath
		data, err := readBoundedRegular(path, maxMarkdownBytes, "corpus Markdown")
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("corpus Markdown is not UTF-8: %s", graphPath)
		}
		data = normalizeMarkdown(data)
		if len(docs) >= maxCorpusDocuments {
			return fmt.Errorf("corpus exceeds %d Markdown files", maxCorpusDocuments)
		}
		if totalBytes+int64(len(data)) > maxCorpusBytes {
			return fmt.Errorf("corpus exceeds %d bytes", maxCorpusBytes)
		}
		totalBytes += int64(len(data))

		docs[graphPath] = document{path: graphPath, data: data}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return docs, nil
}
func checkDocumentBudget(docs map[string]document) error {
	if len(docs) > maxCorpusDocuments {
		return fmt.Errorf("candidate corpus exceeds %d Markdown files", maxCorpusDocuments)
	}
	var totalBytes int64
	for _, doc := range docs {
		totalBytes += int64(len(doc.data))
		if totalBytes > maxCorpusBytes {
			return fmt.Errorf("candidate corpus exceeds %d bytes", maxCorpusBytes)
		}
	}
	return nil
}

func cleanRelative(label, value string) (string, error) {
	if value == "" || filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
		return "", fmt.Errorf("%s must be relative: %q", label, value)
	}
	value = filepath.ToSlash(value)
	if strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("%s must be relative: %q", label, value)
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == ".." {
			return "", fmt.Errorf("%s contains traversal: %q", label, value)
		}
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("%s escapes root: %q", label, value)
	}
	return clean, nil
}

func checkedRelative(label, value string) (string, error) {
	clean, err := cleanRelative(label, value)
	if err != nil {
		return "", err
	}
	return clean, nil
}

func checkedWithin(project, value, label string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s is missing", label)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	abs = filepath.Clean(abs)
	if !sameOrWithin(project, abs) {
		return "", fmt.Errorf("%s escapes project root: %s", label, value)
	}
	if err := noSymlinkPath(project, abs); err != nil {
		return "", err
	}
	return abs, nil
}

func existingDir(path, label string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must not be a symlink: %s", label, path)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory: %s", label, path)
	}
	return nil
}

// noSymlinkPath checks existing path components below root. A not-yet-created
// leaf is allowed for publication and generated outputs, but no existing alias
// can redirect a build outside its declared root.
func noSymlinkPath(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes root: %s", target)
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, statErr := os.Lstat(cur)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component must not be a symlink: %s", cur)
		}
	}
	return nil
}

func readBoundedRegular(path string, limit int64, label string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", label, path, err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s must not be a symlink: %s", label, path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file: %s", label, path)
	}
	if fi.Size() > limit {
		return nil, fmt.Errorf("%s exceeds %d-byte limit: %s", label, limit, path)
	}
	// #nosec G304 -- callers constrain paths to validated roots; checks above reject symlinks and non-regular files.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s changed past %d-byte limit: %s", label, limit, path)
	}
	return data, nil
}

func sameOrWithin(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func rejectPathCollision(paths ...string) error {
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if sameOrWithin(paths[i], paths[j]) || sameOrWithin(paths[j], paths[i]) {
				return fmt.Errorf("authoring/publication roots overlap: %s and %s", paths[i], paths[j])
			}
		}
	}
	return nil
}

func normalizeMarkdown(data []byte) []byte {

	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if s == "" || !strings.HasSuffix(s, "\n") {
		s += "\n"

	}
	return []byte(s)
}
func reservedProjectPath(project, path string) bool {
	rel, err := filepath.Rel(project, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		switch part {
		case ".git", ".substrate", ".poolboy":
			return true
		}
	}
	return false
}
func hiddenDirectory(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts[:len(parts)-1] {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return true
		}
	}
	return false
}

// canonicalPathKey catches case-folded and common decomposed/composed Unicode
// aliases before publication. Filesystems differ on whether those aliases can
// coexist, so rejecting them is safer than publishing ambiguous HTTP paths.
func canonicalPathKey(path string) string {
	return strings.ToLower(normalizeNFC(filepath.ToSlash(path)))
}

func normalizeNFC(s string) string {
	var out []rune
	for _, r := range s {
		if len(out) > 0 {
			if composed, ok := composeLatin(out[len(out)-1], r); ok {
				out[len(out)-1] = composed
				continue
			}
		}
		out = append(out, r)
	}
	return string(out)
}

func composeLatin(base, mark rune) (rune, bool) {
	// Common filename accents cover the forms most often produced by editors;
	// unsupported Unicode normalization is still safe because paths are treated
	// as distinct only when their byte spelling is distinct.
	key := string([]rune{base, mark})
	const acute = '\u0301'
	const grave = '\u0300'
	const diaeresis = '\u0308'
	const tilde = '\u0303'
	const ring = '\u030a'
	const cedilla = '\u0327'
	var table = map[string]rune{
		"a" + string(acute): 'á', "e" + string(acute): 'é', "i" + string(acute): 'í', "o" + string(acute): 'ó', "u" + string(acute): 'ú',
		"A" + string(acute): 'Á', "E" + string(acute): 'É', "I" + string(acute): 'Í', "O" + string(acute): 'Ó', "U" + string(acute): 'Ú',
		"a" + string(grave): 'à', "e" + string(grave): 'è', "i" + string(grave): 'ì', "o" + string(grave): 'ò', "u" + string(grave): 'ù',
		"A" + string(grave): 'À', "E" + string(grave): 'È', "I" + string(grave): 'Ì', "O" + string(grave): 'Ò', "U" + string(grave): 'Ù',
		"a" + string(diaeresis): 'ä', "e" + string(diaeresis): 'ë', "i" + string(diaeresis): 'ï', "o" + string(diaeresis): 'ö', "u" + string(diaeresis): 'ü',
		"A" + string(diaeresis): 'Ä', "E" + string(diaeresis): 'Ë', "I" + string(diaeresis): 'Ï', "O" + string(diaeresis): 'Ö', "U" + string(diaeresis): 'Ü',
		"a" + string(tilde): 'ã', "n" + string(tilde): 'ñ', "o" + string(tilde): 'õ',
		"A" + string(tilde): 'Ã', "N" + string(tilde): 'Ñ', "O" + string(tilde): 'Õ',
		"a" + string(ring): 'å', "A" + string(ring): 'Å',
		"c" + string(cedilla): 'ç', "C" + string(cedilla): 'Ç',
	}
	v, ok := table[key]
	return v, ok
}

func sortedPaths(m map[string]document) []string {
	paths := make([]string, 0, len(m))
	for p := range m {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func checkContext(ctx interface{ Done() <-chan struct{} }) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("build canceled")
	default:
		return nil
	}
}
