// Package source builds a bounded, hashed inventory of an application's source
// files and answers exact provenance questions against it.
//
// The inventory is an inspection boundary, not a guarantee of secret absence:
// it excludes ignored, secret, binary, oversized, symlinked and out-of-tree
// files, records only project-relative paths with byte size, a coarse source
// type and a SHA-256, and never stores contents, timestamps or host paths.
// Scan computes a safe inventory. With no existing lock it writes the initial
// baseline. When a baseline exists, accept must be true to replace it; a
// non-accepting call returns ErrBaselineExists so a caller reviews Drift first.
// Drift compares a current scan without writing anything.
// Affected matches an OKF sources[].resource exactly and returns candidate
// documents for review — never a semantic staleness conclusion.
//
// Scan's write is deliberate: a project has one baseline, and an accidental
// refresh must not hide outstanding source changes.
package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grosspoetrysystems/poolboy/bundle"
)

// Version is the sources.lock.json schema version.
const Version = "0"

// lockName is the inventory file stored under the project's .poolboy state dir.
const lockName = "sources.lock.json"

// ErrBaselineExists means a scan found an existing baseline and was not
// authorized to replace it. Review Drift, then call Scan with accept=true.
var ErrBaselineExists = errors.New("source: baseline already exists; review drift, then scan with accept")

// Inventory is a bounded, deterministic snapshot of an application's source
// files. Files is keyed by project-relative slash path; json.Marshal emits map
// keys sorted, so the serialization is stable.
type Inventory struct {
	Version  string           `json:"version"`
	Revision string           `json:"revision,omitempty"`
	Files    map[string]Entry `json:"files"`
}

// Entry is one inventoried source file. It carries no path, timestamp or
// content — only what a drift comparison and a review need.
type Entry struct {
	Type   string `json:"type"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// ChangeKind classifies a drift observation. It is retained as a small
// vocabulary while Change exposes the CLI-facing status string.
type ChangeKind string

const (
	// Added is present in the current scan, absent from the baseline.
	Added ChangeKind = "added"
	// Removed is present in the baseline, absent from the current scan.
	Removed ChangeKind = "removed"
	// Modified is present in both with a different SHA-256.
	Modified ChangeKind = "modified"
)

// Change is one drift observation between the baseline lock and a fresh scan.
// It is evidence for review, not a definite staleness verdict.
type Change struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// projectRoot validates the root that anchors every source operation. Bundle
// discovery supplies an absolute directory; rejecting relative or symlink roots
// keeps state writes and returned paths contained and deterministic.
func projectRoot(b *bundle.Bundle) (string, error) {
	if b == nil || b.Root == "" {
		return "", errors.New("source: project root is required")
	}
	root := filepath.Clean(b.Root)
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("source: project root must be absolute: %q", b.Root)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("source: project root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("source: project root must not be a symlink")
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source: project root is not a directory: %s", root)
	}
	return root, nil
}

// statePaths validates and returns the contained state directory and lock
// paths. It does not create the directory when create is false.
func statePaths(b *bundle.Bundle, create bool) (string, string, error) {
	root, err := projectRoot(b)
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(root, ".poolboy")
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("source: state path escapes project root")
	}
	info, statErr := os.Lstat(dir)
	if statErr != nil {
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", "", fmt.Errorf("source: state directory: %w", statErr)
		}
		if !create {
			return dir, filepath.Join(dir, lockName), nil
		}
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", "", fmt.Errorf("source: creating state dir: %w", err)
		}
		info, statErr = os.Lstat(dir)
		if statErr != nil {
			return "", "", fmt.Errorf("source: state directory: %w", statErr)
		}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", "", errors.New("source: .poolboy must be a real directory")
	}
	return dir, filepath.Join(dir, lockName), nil
}

// ReadLock loads the baseline inventory. A missing file surfaces as an
// fs.ErrNotExist-wrapped error so callers can distinguish "never scanned".
func ReadLock(b *bundle.Bundle) (*Inventory, error) {
	_, lock, err := statePaths(b, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(lock)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("source: sources.lock.json must be a regular file")
	}
	// #nosec G304 -- lock is the validated .poolboy path under projectRoot.
	data, err := os.ReadFile(lock)
	if err != nil {
		return nil, err
	}
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, fmt.Errorf("source: parsing %s: %w", lockName, err)
	}
	if inv.Version != Version {
		return nil, fmt.Errorf("source: unsupported inventory version %q", inv.Version)
	}
	if inv.Files == nil {
		inv.Files = map[string]Entry{}
	}
	for path := range inv.Files {
		if !validProjectPath(path) {
			return nil, fmt.Errorf("source: invalid inventory path %q", path)
		}
	}
	return &inv, nil
}

// WriteLock atomically replaces the inventory at
// <root>/.poolboy/sources.lock.json. The temp file is created inside the same
// state directory and renamed into place, so the write is atomic and always
// contained under the project root.
func WriteLock(b *bundle.Bundle, inv *Inventory) error {
	return writeLock(b, inv, false)
}

// writeLock publishes a complete lock file. createOnly uses a hard link from
// the closed temp file so an initial scan cannot replace a baseline another
// scan installed while it was walking the project.
func writeLock(b *bundle.Bundle, inv *Inventory, createOnly bool) error {
	dir, lock, err := statePaths(b, true)
	if err != nil {
		return err
	}
	data, err := marshalLock(inv)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, lockName+".tmp-*")
	if err != nil {
		return fmt.Errorf("source: creating temp lock: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("source: writing temp lock: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("source: closing temp lock: %w", err)
	}
	if createOnly {
		if err := os.Link(tmpName, lock); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%w: %s", ErrBaselineExists, filepath.Join(".poolboy", lockName))
			}
			return fmt.Errorf("source: creating lock: %w", err)
		}
		return nil
	}
	if err := os.Rename(tmpName, lock); err != nil {
		return fmt.Errorf("source: replacing lock: %w", err)
	}
	return nil
}

// marshalLock renders the deterministic lock bytes: sorted keys (json map
// encoding), two-space indent, final LF. No timestamps or host paths enter it.
func marshalLock(inv *Inventory) ([]byte, error) {
	if inv == nil {
		return nil, errors.New("source: inventory is required")
	}
	snapshot := *inv
	if snapshot.Version == "" {
		snapshot.Version = Version
	}
	if snapshot.Version != Version {
		return nil, fmt.Errorf("source: unsupported inventory version %q", snapshot.Version)
	}
	if snapshot.Files == nil {
		snapshot.Files = map[string]Entry{}
	}
	for path := range snapshot.Files {
		if !validProjectPath(path) {
			return nil, fmt.Errorf("source: invalid inventory path %q", path)
		}
	}
	data, err := json.MarshalIndent(&snapshot, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("source: encoding inventory: %w", err)
	}
	return append(data, '\n'), nil
}

// validProjectPath accepts only normalized project-relative slash paths.
func validProjectPath(path string) bool {
	if path == "" || filepath.IsAbs(filepath.FromSlash(path)) ||
		strings.HasPrefix(path, "/") || strings.Contains(path, "\\") ||
		strings.Contains(path, "//") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// Scan computes a safe inventory. With no existing lock it writes the initial
// baseline. When a baseline exists, accept must be true to replace it; a
// non-accepting call returns ErrBaselineExists so a caller reviews Drift first.
// The lock is replaced only after the current scan succeeds.
func Scan(b *bundle.Bundle, accept bool) (*Inventory, error) {
	if b != nil && len(b.Unknown) > 0 {
		unknown := append([]string(nil), b.Unknown...)
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown poolboy.toml setting: %s", strings.Join(unknown, ", "))
	}
	_, lock, err := statePaths(b, false)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		info, statErr := os.Lstat(lock)
		switch {
		case statErr == nil:
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil, errors.New("source: sources.lock.json must be a regular file")
			}
			if !accept {
				return nil, fmt.Errorf("%w: %s", ErrBaselineExists, filepath.Join(".poolboy", lockName))
			}
		case !errors.Is(statErr, fs.ErrNotExist):
			return nil, fmt.Errorf("source: checking baseline: %w", statErr)
		}
	}
	inv, err := scanCurrent(b)
	if err != nil {
		return nil, err
	}
	root, err := projectRoot(b)
	if err != nil {
		return nil, err
	}
	inv.Version = Version
	inv.Revision = gitRevision(root)
	if err := writeLock(b, inv, !accept); err != nil {
		return nil, err
	}
	return inv, nil
}

// Drift compares the written baseline inventory against a fresh safe scan and
// reports added, removed and modified source files. It never rewrites the
// baseline: the comparison is read-only against the recorded hashes.
func Drift(b *bundle.Bundle) ([]Change, error) {
	base, err := ReadLock(b)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("source: no baseline inventory at %s; run scan first",
				filepath.Join(".poolboy", lockName))
		}
		return nil, err
	}
	cur, err := scanCurrent(b)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for path, be := range base.Files {
		ce, ok := cur.Files[path]
		if !ok {
			changes = append(changes, Change{Path: path, Status: string(Removed)})
			continue
		}
		if ce.SHA256 != be.SHA256 {
			changes = append(changes, Change{Path: path, Status: string(Modified)})
		}
	}
	for path := range cur.Files {
		if _, ok := base.Files[path]; !ok {
			changes = append(changes, Change{Path: path, Status: string(Added)})
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path != changes[j].Path {
			return changes[i].Path < changes[j].Path
		}
		return changes[i].Status < changes[j].Status
	})
	return changes, nil
}

// gitRevision returns the current HEAD commit if root is a Git work tree, or ""
// otherwise. It is best-effort provenance: no Git, no repo, or any error yields
// an empty revision rather than a scan failure. A commit id is not a host path.
func gitRevision(root string) string {
	// #nosec G204 -- root is validated as an absolute project directory; git receives fixed arguments and no shell.
	cmd := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
