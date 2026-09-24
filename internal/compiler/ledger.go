package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const ledgerVersion = "0"

type ledger struct {
	Version string                 `json:"version"`
	Files   map[string]ledgerEntry `json:"files"`
}

type ledgerEntry struct {
	Data     string `json:"data"`
	SHA256   string `json:"sha256"`
	Template string `json:"template"`
}

type fileBackup struct {
	path   string
	data   []byte
	exists bool
}

func loadLedger(project string) (ledger, error) {
	state := filepath.Join(project, ".poolboy")
	if fi, err := os.Lstat(state); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return ledger{}, fmt.Errorf("state directory must not be a symlink: %s", state)
		}
		if !fi.IsDir() {
			return ledger{}, fmt.Errorf("state path is not a directory: %s", state)
		}
	} else if !os.IsNotExist(err) {
		return ledger{}, fmt.Errorf("state directory: %w", err)
	}
	path := filepath.Join(state, "generated.json")
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return ledger{Version: ledgerVersion, Files: map[string]ledgerEntry{}}, nil
	}
	if err != nil {
		return ledger{}, fmt.Errorf("generated ledger: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return ledger{}, fmt.Errorf("generated ledger must not be a symlink: %s", path)
	}
	data, err := readBoundedRegular(path, 2<<20, "generated ledger")
	if err != nil {
		return ledger{}, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	var got ledger
	if err := dec.Decode(&got); err != nil {
		return ledger{}, fmt.Errorf("decode generated ledger: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return ledger{}, fmt.Errorf("decode generated ledger: multiple JSON values")
		}
		return ledger{}, fmt.Errorf("decode generated ledger: trailing data: %w", err)
	}
	if got.Version != "" && got.Version != ledgerVersion {
		return ledger{}, fmt.Errorf("unsupported generated ledger version %q", got.Version)
	}
	if got.Files == nil {
		got.Files = map[string]ledgerEntry{}
	}
	got.Version = ledgerVersion
	seen := map[string]string{}
	for path, entry := range got.Files {
		clean, err := cleanRelative("generated ledger path", path)
		if err != nil || !strings.HasSuffix(strings.ToLower(clean), ".md") {
			return ledger{}, fmt.Errorf("invalid generated ledger path %q", path)
		}
		if prior, ok := seen[canonicalPathKey(clean)]; ok && prior != clean {
			return ledger{}, fmt.Errorf("generated ledger path collision: %q and %q", prior, clean)
		}
		seen[canonicalPathKey(clean)] = clean
		if len(entry.SHA256) != sha256.Size*2 {
			return ledger{}, fmt.Errorf("invalid generated ledger hash for %q", path)
		}
		if _, err := hex.DecodeString(entry.SHA256); err != nil {
			return ledger{}, fmt.Errorf("invalid generated ledger hash for %q: %w", path, err)
		}
		if clean != path {
			delete(got.Files, path)
			got.Files[clean] = entry
		}
	}
	return got, nil
}

func desiredLedger(renders []rendered) ledger {
	files := make(map[string]ledgerEntry, len(renders))
	for _, rendered := range renders {
		files[rendered.rel] = ledgerEntry{
			Data:     rendered.dataPath,
			SHA256:   hashBytes(rendered.data),
			Template: rendered.templatePath,
		}
	}
	return ledger{Version: ledgerVersion, Files: files}
}

// planLedger checks whether existing materialized files may be replaced and
// returns unchanged stale generated files that can be safely removed. A file
// without a ledger entry is handwritten by definition and is never overwritten.
func planLedger(existing map[string]document, old, next ledger) (map[string]bool, error) {
	stale := map[string]bool{}
	for rel, previous := range old.Files {
		path := "/" + filepath.ToSlash(rel)
		current, exists := existing[path]
		if _, wanted := next.Files[rel]; wanted {
			if !exists {
				continue
			}
			if hashBytes(current.data) != previous.SHA256 {
				return nil, fmt.Errorf("generated file was edited outside its template: %s", path)
			}
			continue
		}
		if !exists {
			continue
		}
		if hashBytes(current.data) != previous.SHA256 {
			return nil, fmt.Errorf("stale generated file was edited; refusing removal: %s", path)
		}
		stale[rel] = true
	}
	for rel := range next.Files {
		path := "/" + filepath.ToSlash(rel)
		if _, exists := existing[path]; !exists {
			continue
		}

		if _, tracked := old.Files[rel]; !tracked {
			return nil, fmt.Errorf("render output would overwrite handwritten Markdown: %s", path)
		}
	}
	return stale, nil
}

// verifyRenderTargets re-checks each target on disk after the corpus walk.
// The walk intentionally omits publication and hidden subtrees, so relying on
// its map alone could mistake a handwritten file for a missing generated file.
func verifyRenderTargets(corpus string, existing map[string]document, old ledger, renders []rendered) error {
	for _, render := range renders {
		rel := filepath.ToSlash(render.rel)
		path := filepath.Join(corpus, filepath.FromSlash(rel))
		if err := noSymlinkPath(corpus, path); err != nil {
			return err
		}
		fi, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect render output %s: %w", rel, err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("render output is not a regular file: %s", path)
		}
		data, err := readBoundedRegular(path, maxMarkdownBytes, "render output")
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("render output is not UTF-8: %s", path)
		}
		data = normalizeMarkdown(data)
		graphPath := "/" + rel
		previous, tracked := old.Files[rel]
		current, indexed := existing[graphPath]
		if !tracked && !indexed {
			return fmt.Errorf("render output would overwrite handwritten Markdown: %s", graphPath)
		}
		if tracked && hashBytes(data) != previous.SHA256 {
			return fmt.Errorf("generated file was edited outside its template: %s", graphPath)
		}
		if indexed && tracked && hashBytes(current.data) != previous.SHA256 {
			return fmt.Errorf("generated file was edited outside its template: %s", graphPath)
		}
		existing[graphPath] = document{path: graphPath, data: data}
	}
	return nil
}
func materializeGenerated(project, corpus string, existing map[string]document, stale map[string]bool, next ledger, renders []rendered) (func() error, error) {
	var backups []fileBackup
	rollback := func() error { return restoreBackups(backups) }
	fail := func(err error) (func() error, error) {
		if rollbackErr := rollback(); rollbackErr != nil {
			return rollback, errors.Join(err, fmt.Errorf("rollback generated files: %w", rollbackErr))
		}
		return rollback, err
	}
	paths := make([]string, 0, len(stale)+len(next.Files))
	for rel := range stale {
		paths = append(paths, rel)
	}
	for rel := range next.Files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	seen := map[string]bool{}
	byPath := make(map[string]rendered, len(renders))
	for _, item := range renders {
		byPath[item.rel] = item
	}
	for _, rel := range paths {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		path := filepath.Join(corpus, filepath.FromSlash(rel))
		if err := noSymlinkPath(corpus, path); err != nil {
			return fail(err)
		}
		old, existed := existing["/"+filepath.ToSlash(rel)]
		backups = append(backups, fileBackup{path: path, data: old.data, exists: existed})
		if stale[rel] {
			if err := os.Remove(path); err != nil {
				return fail(fmt.Errorf("remove stale generated file %s: %w", path, err))
			}
			continue
		}
		item, ok := byPath[rel]
		if !ok {
			return fail(fmt.Errorf("missing render output for ledger path %s", rel))
		}
		if err := writeAtomic(path, item.data, 0o644); err != nil {
			return fail(fmt.Errorf("materialize generated file %s: %w", path, err))
		}
	}
	state := filepath.Join(project, ".poolboy")
	if err := noSymlinkPath(project, state); err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return fail(fmt.Errorf("create state directory: %w", err))
	}
	return rollback, nil
}

func restoreBackups(backups []fileBackup) error {
	var first error
	for i := len(backups) - 1; i >= 0; i-- {
		backup := backups[i]
		if backup.exists {
			if err := writeAtomic(backup.path, backup.data, 0o644); err != nil && first == nil {
				first = err
			}
			continue
		}
		if err := os.Remove(backup.path); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	return first
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	parent := filepath.Dir(path)
	// #nosec G301 -- generated corpus directories intentionally expose published Markdown.
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	name, err := os.CreateTemp(parent, ".poolboy-write-*")
	if err != nil {
		return err
	}
	tmp := name.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := name.Chmod(mode); err != nil {
		_ = name.Close()
		return err
	}
	if _, err := name.Write(data); err != nil {
		_ = name.Close()
		return err
	}
	if err := name.Sync(); err != nil {
		_ = name.Close()
		return err
	}
	if err := name.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func encodeLedger(l ledger) ([]byte, error) {
	if l.Files == nil {
		l.Files = map[string]ledgerEntry{}
	}
	// encoding/json sorts map keys; sort is explicit here to make this contract
	// obvious and keep the output stable if the encoder's map behavior changes.
	keys := make([]string, 0, len(l.Files))
	for key := range l.Files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]ledgerEntry, len(keys))
	for _, key := range keys {
		ordered[key] = l.Files[key]
	}
	l.Files = ordered
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
