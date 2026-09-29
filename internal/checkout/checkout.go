// Package checkout manages asynchronous Markdown editing sessions.
package checkout

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/index"
	"github.com/grosspoetrysystems/poolboy/internal/source"
)

const (
	stateVersion       = "0"
	markerName         = ".poolboy-checkout.json"
	maxMarkdownBytes   = 8 << 20
	maxCorpusBytes     = 64 << 20
	maxCorpusDocuments = 10_000
)

// Revision identifies exact file bytes.
type Revision struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Change describes one file-level variance from the checkout base.
type Change struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// Conflict explains why a draft change cannot be applied.
type Conflict struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Created describes a new asynchronous checkout.
type Created struct {
	ID          string             `json:"id"`
	Path        string             `json:"path"`
	Documents   int                `json:"documents"`
	Quarantined []source.Exclusion `json:"quarantined"`
}

// Plan is the deterministic three-way check-in result.
type Plan struct {
	ID                   string             `json:"id"`
	Path                 string             `json:"path"`
	DraftChanges         []Change           `json:"draft_changes"`
	WorkspaceChanges     []Change           `json:"workspace_changes"`
	SourceChanges        []Change           `json:"source_changes"`
	SourceQuarantined    []source.Exclusion `json:"source_quarantined"`
	GeneratorChanges     []Change           `json:"generator_changes"`
	Conflicts            []Conflict         `json:"conflicts"`
	RequiresSourceReview bool               `json:"requires_source_review"`
	CanApply             bool               `json:"can_apply"`
	Applied              bool               `json:"applied"`
}

type state struct {
	Version   string              `json:"version"`
	ID        string              `json:"id"`
	Path      string              `json:"path"`
	Corpus    map[string]Revision `json:"corpus"`
	Sources   map[string]Revision `json:"sources"`
	Generated map[string]Revision `json:"generated"`
}

type marker struct {
	Version string `json:"version"`
	ID      string `json:"id"`
}

type document struct {
	Revision
	data []byte
}

type action struct {
	path   string
	data   []byte
	remove bool
}

// Create copies current Markdown to a new directory and records private base state.
func Create(b *bundle.Bundle, target string) (*Created, error) {
	path, err := checkoutPath(b, target)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("checkout: target already exists: %s", path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("checkout: inspect target: %w", err)
	}

	docs, err := readDocuments(b, b.Dir)
	if err != nil {
		return nil, err
	}
	sources, err := source.Current(b)
	if err != nil {
		return nil, fmt.Errorf("checkout: observe source state: %w", err)
	}
	generated, err := readGenerated(b.Root)
	if err != nil {
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("checkout: create id: %w", err)
	}

	// #nosec G301 -- checkout directories are user-facing ordinary Markdown.
	if err := os.Mkdir(path, 0o755); err != nil {
		return nil, fmt.Errorf("checkout: create target: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(path)
			_ = os.Remove(statePath(b.Root, id))
		}
	}()
	for _, rel := range sortedDocumentPaths(docs) {
		if err := writeAtomic(filepath.Join(path, filepath.FromSlash(rel)), docs[rel].data, 0o644); err != nil {
			return nil, fmt.Errorf("checkout: copy %s: %w", rel, err)
		}
	}
	if err := writeJSONAtomic(filepath.Join(path, markerName), marker{Version: stateVersion, ID: id}, 0o600); err != nil {
		return nil, fmt.Errorf("checkout: write marker: %w", err)
	}
	st := state{
		Version:   stateVersion,
		ID:        id,
		Path:      path,
		Corpus:    revisions(docs),
		Sources:   sourceRevisions(sources),
		Generated: generated,
	}
	if err := os.MkdirAll(filepath.Dir(statePath(b.Root, id)), 0o700); err != nil {
		return nil, fmt.Errorf("checkout: create private state directory: %w", err)
	}
	if err := writeJSONAtomic(statePath(b.Root, id), st, 0o600); err != nil {
		return nil, fmt.Errorf("checkout: write state: %w", err)
	}
	cleanup = false
	return &Created{ID: id, Path: path, Documents: len(docs), Quarantined: sources.Quarantined}, nil
}

// Checkin previews or applies a file-level three-way plan for target.
func Checkin(b *bundle.Bundle, target string, apply bool) (*Plan, error) {
	path, err := existingCheckoutPath(target)
	if err != nil {
		return nil, err
	}
	var mark marker
	if err := readJSON(filepath.Join(path, markerName), &mark); err != nil {
		return nil, fmt.Errorf("checkin: read checkout marker: %w", err)
	}
	if mark.Version != stateVersion || !validID(mark.ID) {
		return nil, fmt.Errorf("checkin: unsupported checkout marker")
	}
	var base state
	stateFile := statePath(b.Root, mark.ID)
	if err := readJSON(stateFile, &base); err != nil {
		return nil, fmt.Errorf("checkin: read private checkout state: %w", err)
	}
	if base.Version != stateVersion || base.ID != mark.ID || filepath.Clean(base.Path) != path {
		return nil, fmt.Errorf("checkin: checkout state does not match %s", path)
	}

	draftBundle := *b
	draftBundle.Dir = path
	draft, err := readDocuments(&draftBundle, path)
	if err != nil {
		return nil, fmt.Errorf("checkin: read draft: %w", err)
	}
	current, err := readDocuments(b, b.Dir)
	if err != nil {
		return nil, fmt.Errorf("checkin: read workspace: %w", err)
	}
	currentSources, err := source.Current(b)
	if err != nil {
		return nil, fmt.Errorf("checkin: observe source state: %w", err)
	}
	currentGenerated, err := readGenerated(b.Root)
	if err != nil {
		return nil, err
	}

	plan := &Plan{
		ID:                mark.ID,
		Path:              path,
		DraftChanges:      compare(base.Corpus, revisions(draft)),
		WorkspaceChanges:  compare(base.Corpus, revisions(current)),
		SourceChanges:     compare(base.Sources, sourceRevisions(currentSources)),
		SourceQuarantined: currentSources.Quarantined,
		GeneratorChanges:  compare(base.Generated, currentGenerated),
		Conflicts:         []Conflict{},
	}
	plan.RequiresSourceReview = len(plan.SourceChanges) > 0 || len(plan.SourceQuarantined) > 0
	actions := planActions(base.Corpus, draft, current, base.Generated, currentGenerated, &plan.Conflicts)
	plan.CanApply = len(plan.Conflicts) == 0
	if !apply || !plan.CanApply {
		return plan, nil
	}
	if err := applyActions(b.Dir, actions, current); err != nil {
		return nil, fmt.Errorf("checkin: apply: %w", err)
	}
	plan.Applied = true
	if err := os.Remove(stateFile); err != nil {
		return plan, fmt.Errorf("checkin: corpus applied but checkout state could not be closed: %w", err)
	}
	return plan, nil
}

func planActions(base map[string]Revision, draft, current map[string]document, baseGenerated, currentGenerated map[string]Revision, conflicts *[]Conflict) []action {
	paths := unionPaths(base, revisions(draft), revisions(current))
	actions := make([]action, 0)
	for _, path := range paths {
		baseRev, baseOK := base[path]
		draftDoc, draftOK := draft[path]
		currentDoc, currentOK := current[path]
		draftChanged := !same(baseRev, baseOK, draftDoc.Revision, draftOK)
		currentChanged := !same(baseRev, baseOK, currentDoc.Revision, currentOK)
		if !draftChanged || same(draftDoc.Revision, draftOK, currentDoc.Revision, currentOK) {
			continue
		}
		if _, wasGenerated := baseGenerated[path]; wasGenerated {
			*conflicts = append(*conflicts, Conflict{Path: path, Reason: "draft changes a generated document; edit its template or data"})
			continue
		}
		if _, isGenerated := currentGenerated[path]; isGenerated {
			*conflicts = append(*conflicts, Conflict{Path: path, Reason: "draft changes a generated document; edit its template or data"})
			continue
		}
		if currentChanged {
			*conflicts = append(*conflicts, Conflict{Path: path, Reason: "draft and workspace both changed from the checkout base"})
			continue
		}
		actions = append(actions, action{path: path, data: draftDoc.data, remove: !draftOK})
	}
	return actions
}

func applyActions(root string, actions []action, current map[string]document) error {
	type backup struct {
		path   string
		data   []byte
		exists bool
	}
	backups := make([]backup, 0, len(actions))
	rollback := func() error {
		var rollbackErr error
		for i := len(backups) - 1; i >= 0; i-- {
			item := backups[i]
			path := filepath.Join(root, filepath.FromSlash(item.path))
			if item.exists {
				if err := writeAtomic(path, item.data, 0o644); err != nil {
					rollbackErr = errors.Join(rollbackErr, err)
				}
			} else if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		return rollbackErr
	}
	fail := func(err error) error {
		if rollbackErr := rollback(); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
		return err
	}
	for _, item := range actions {
		if err := safeRelativePath(root, item.path); err != nil {
			return fail(err)
		}
		expected, expectedExists := current[item.path]
		observed, observedExists, err := readActionTarget(root, item.path)
		if err != nil {
			return fail(err)
		}
		if !same(expected.Revision, expectedExists, observed.Revision, observedExists) {
			return fail(fmt.Errorf("workspace changed while applying %s", item.path))
		}
		backups = append(backups, backup{path: item.path, data: observed.data, exists: observedExists})
		path := filepath.Join(root, filepath.FromSlash(item.path))
		if item.remove {
			err = os.Remove(path)
		} else {
			err = writeAtomic(path, item.data, 0o644)
		}
		if err != nil {
			return fail(err)
		}
	}
	return nil
}

func readActionTarget(root, rel string) (document, bool, error) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return document{}, false, nil
	}
	if err != nil {
		return document{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return document{}, false, fmt.Errorf("checkin: unsupported corpus path: %s", path)
	}
	data, err := readBounded(path, maxMarkdownBytes)
	if err != nil {
		return document{}, false, err
	}
	return document{Revision: revision(data), data: data}, true, nil
}

func readDocuments(b *bundle.Bundle, root string) (map[string]document, error) {
	docs := map[string]document{}
	seen := map[string]string{}
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("checkout: corpus contains symlink: %s", path)
		}
		if entry.IsDir() {
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") || index.MatchIgnore(b, path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		key := strings.ToLower(rel)
		if prior, ok := seen[key]; ok {
			return fmt.Errorf("checkout: corpus path collision: %s and %s", prior, rel)
		}
		data, err := readBounded(path, maxMarkdownBytes)
		if err != nil {
			return err
		}
		if len(docs) >= maxCorpusDocuments || total+int64(len(data)) > maxCorpusBytes {
			return errors.New("checkout: corpus exceeds supported size")
		}
		total += int64(len(data))
		seen[key] = rel
		docs[rel] = document{Revision: revision(data), data: data}
		return nil
	})
	return docs, err
}

func checkoutPath(b *bundle.Bundle, target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("checkout: target directory is required")
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("checkout: resolve target: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("checkout: target parent must exist: %w", err)
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	root, err := filepath.EvalSymlinks(b.Root)
	if err != nil {
		return "", fmt.Errorf("checkout: resolve project root: %w", err)
	}
	if within(root, abs) {
		return "", errors.New("checkout: target must be outside the project root")
	}
	return abs, nil
}

func existingCheckoutPath(target string) (string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("checkin: resolve checkout: %w", err)
	}
	path, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("checkin: resolve checkout: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("checkin: inspect checkout: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("checkin: checkout must be a real directory")
	}
	return filepath.Clean(path), nil
}

func statePath(root, id string) string {
	return filepath.Join(root, ".poolboy", "checkouts", id+".json")
}

func readGenerated(root string) (map[string]Revision, error) {
	var ledger struct {
		Version string `json:"version"`
		Files   map[string]struct {
			Data     string `json:"data"`
			SHA256   string `json:"sha256"`
			Template string `json:"template"`
		} `json:"files"`
	}
	path := filepath.Join(root, ".poolboy", "generated.json")
	if err := readJSON(path, &ledger); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]Revision{}, nil
		}
		return nil, fmt.Errorf("checkout: read generated ledger: %w", err)
	}
	if ledger.Version != stateVersion {
		return nil, errors.New("checkout: unsupported generated ledger")
	}
	result := make(map[string]Revision, len(ledger.Files))
	for path, entry := range ledger.Files {
		result[filepath.ToSlash(path)] = Revision{SHA256: entry.SHA256}
	}
	return result, nil
}

func sourceRevisions(inventory *source.Inventory) map[string]Revision {
	result := make(map[string]Revision, len(inventory.Files))
	for path, entry := range inventory.Files {
		result[path] = Revision{Bytes: entry.Bytes, SHA256: entry.SHA256}
	}
	return result
}

func revisions(docs map[string]document) map[string]Revision {
	result := make(map[string]Revision, len(docs))
	for path, doc := range docs {
		result[path] = doc.Revision
	}
	return result
}

func compare(before, after map[string]Revision) []Change {
	paths := unionPaths(before, after)
	changes := make([]Change, 0)
	for _, path := range paths {
		old, oldOK := before[path]
		now, nowOK := after[path]
		if same(old, oldOK, now, nowOK) {
			continue
		}
		status := "modified"
		if !oldOK {
			status = "added"
		} else if !nowOK {
			status = "removed"
		}
		changes = append(changes, Change{Path: path, Status: status})
	}
	return changes
}

func unionPaths(sets ...map[string]Revision) []string {
	all := map[string]bool{}
	for _, set := range sets {
		for path := range set {
			all[path] = true
		}
	}
	paths := make([]string, 0, len(all))
	for path := range all {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedDocumentPaths(docs map[string]document) []string {
	paths := make([]string, 0, len(docs))
	for path := range docs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func same(a Revision, aOK bool, b Revision, bOK bool) bool {
	return aOK == bOK && (!aOK || a == b)
}

func revision(data []byte) Revision {
	sum := sha256.Sum256(data)
	return Revision{Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func randomID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func validID(id string) bool {
	if len(id) != 32 || strings.ToLower(id) != id {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	// #nosec G304 -- path was reached by corpus traversal or Lstat-validated under the corpus root.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("checkout: unsupported Markdown file: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("checkout: Markdown exceeds %d bytes: %s", limit, path)
	}
	return data, nil
}

func readJSON(path string, value any) error {
	data, err := readBoundedJSON(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("checkout: state file contains trailing data")
	}
	return nil
}

func readBoundedJSON(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxCorpusBytes {
		return nil, fmt.Errorf("checkout: unsupported state file: %s", path)
	}
	// #nosec G304 -- path is a fixed private state, marker, or generated-ledger path checked by Lstat.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxCorpusBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxCorpusBytes {
		return nil, fmt.Errorf("checkout: state exceeds %d bytes: %s", maxCorpusBytes, path)
	}
	return data, nil
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), mode)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	parent := filepath.Dir(path)
	// #nosec G301 -- checkout and canonical corpus directories are user-facing Markdown.
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(parent, ".poolboy-write-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func safeRelativePath(root, rel string) error {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("checkin: invalid corpus path %q", rel)
	}
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("checkin: symlink path is not allowed: %s", current)
		}
	}
	return nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
