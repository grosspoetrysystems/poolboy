// Package compiler validates a Poolboy corpus, runs configured templates, and
// atomically publishes a deterministic static corpus.
package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/internal/renderer"
	"github.com/grosspoetrysystems/poolboy/parse"
)

type rendered struct {
	rel          string
	templatePath string
	dataPath     string
	data         []byte
}

// Build validates and publishes b into its configured output directory. All
// validation and rendering happens before authoring files or the existing
// publication are changed. A failed build leaves the previous publication
// untouched and never returns partial generated Markdown.
func Build(ctx context.Context, b *bundle.Bundle, rendererPath string) (buildErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	r, err := validateRoots(b)
	if err != nil {
		return err
	}
	if err := validateExistingPublication(r.output); err != nil {
		return err
	}
	if strings.TrimSpace(b.Spec) != "0.2" {
		return fmt.Errorf("unsupported Poolboy spec %q; build requires 0.2", b.Spec)
	}
	if len(b.Unknown) > 0 {
		unknown := append([]string(nil), b.Unknown...)
		sort.Strings(unknown)
		return fmt.Errorf("unknown configuration setting: %s", strings.Join(unknown, ", "))
	}
	if err := validateMappings(b, r); err != nil {
		return err
	}
	landing, err := resolveLanding(b, r)
	if err != nil {
		return err
	}
	oldLedger, err := loadLedger(r.project)
	if err != nil {
		return err
	}
	existing, err := collectCorpus(r)
	if err != nil {
		return err
	}
	if err := checkCandidateCollisions(existing); err != nil {
		return err
	}

	var outputs []rendered
	var companion string
	if len(b.Renders) > 0 {
		companion, err = renderer.Resolve(rendererPath)
		if err != nil {
			return err
		}
		if rendererPath != "" && !filepath.IsAbs(rendererPath) {
			return fmt.Errorf("renderer path must be absolute: %q", rendererPath)
		}
		outputs, err = renderDocuments(ctx, b, companion)
		if err != nil {
			return err
		}
	}
	newLedger := desiredLedger(outputs)
	if err := verifyRenderTargets(r.corpus, existing, oldLedger, outputs); err != nil {
		return err
	}
	stale, err := planLedger(existing, oldLedger, newLedger)
	if err != nil {
		return err
	}
	candidate := cloneDocuments(existing)
	for rel := range stale {
		delete(candidate, "/"+filepath.ToSlash(rel))
	}
	for _, output := range outputs {
		path := "/" + filepath.ToSlash(output.rel)
		if err := ensureCandidatePath(candidate, path); err != nil {
			return err
		}
		candidate[path] = document{path: path, data: output.data}
	}
	if err := checkDocumentBudget(candidate); err != nil {
		return err
	}
	if err := checkCandidateCollisions(candidate); err != nil {
		return err
	}
	for path, doc := range candidate {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if err := validateOKF(path, doc.data); err != nil {
			return err
		}
	}

	corpusStage, err := os.MkdirTemp(filepath.Dir(r.corpus), ".poolboy-corpus-build-*")
	if err != nil {
		return fmt.Errorf("stage corpus: %w", err)
	}
	defer func() { _ = os.RemoveAll(corpusStage) }()
	if err := writeDocuments(corpusStage, candidate); err != nil {
		return err
	}
	stagedIndex, err := stageIndex(b, corpusStage)
	if err != nil {
		return fmt.Errorf("index candidate corpus: %w", err)
	}
	artifacts, err := buildLandingArtifacts(ctx, landing, candidate)
	if err != nil {
		return err
	}
	llmsBytes := renderLLMS(b.Name)
	graphBytes, err := buildGraph(stagedIndex, candidate, artifacts, llmsBytes)
	if err != nil {
		return err
	}

	publicationParent := filepath.Dir(r.output)
	// #nosec G301 -- publication directories intentionally expose static output.
	if err := os.MkdirAll(publicationParent, 0o755); err != nil {
		return fmt.Errorf("create publication parent: %w", err)
	}
	if err := noSymlinkPath(r.project, publicationParent); err != nil {
		return err
	}
	publicationStage, err := os.MkdirTemp(publicationParent, ".poolboy-publication-*")
	if err != nil {
		return fmt.Errorf("stage publication: %w", err)
	}
	defer func() { _ = os.RemoveAll(publicationStage) }()
	if err := writeDocuments(publicationStage, candidate); err != nil {
		return err
	}
	if err := writeLandingArtifacts(publicationStage, artifacts); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(publicationStage, "graph.json"), graphBytes, 0o644); err != nil {
		return fmt.Errorf("stage graph.json: %w", err)
	}
	if err := writeFile(filepath.Join(publicationStage, "llms.txt"), llmsBytes, 0o644); err != nil {
		return fmt.Errorf("stage llms.txt: %w", err)
	}
	if err := checkContext(ctx); err != nil {
		return err
	}

	rollbackGenerated, err := materializeGenerated(r.project, r.corpus, existing, stale, newLedger, outputs)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if err := rollbackGenerated(); err != nil {
				buildErr = errors.Join(buildErr, fmt.Errorf("rollback generated files: %w", err))
			}
		}
	}()

	ledgerBytes, err := encodeLedger(newLedger)
	if err != nil {
		return err
	}
	ledgerTemp, err := stageLedger(r.project, ledgerBytes)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(ledgerTemp) }()
	if err := validateExistingPublication(r.output); err != nil {
		return err
	}

	swap, err := swapPublication(publicationStage, r.output)
	if err != nil {
		return err
	}
	if err := os.Rename(ledgerTemp, filepath.Join(r.project, ".poolboy", "generated.json")); err != nil {
		if restoreErr := swap.restore(); restoreErr != nil {
			return errors.Join(
				fmt.Errorf("publish generated ledger: %w", err),
				fmt.Errorf("restore publication: %w", restoreErr),
			)
		}
		return fmt.Errorf("publish generated ledger: %w", err)
	}
	// The new output and ledger are committed. A failure to remove the old
	// backup is harmless and must not roll back the successfully published set.
	_ = swap.commit()
	committed = true
	return nil
}

func renderDocuments(ctx context.Context, b *bundle.Bundle, companion string) ([]rendered, error) {
	out := make([]rendered, 0, len(b.Renders))
	for i, mapping := range b.Renders {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		templatePath := filepath.Join(b.Root, filepath.FromSlash(mapping.Template))
		dataPath := filepath.Join(b.Root, filepath.FromSlash(mapping.Data))
		templateBytes, err := readBoundedRegular(templatePath, maxTemplateBytes, "render template")
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(templateBytes) || utf16Length(string(templateBytes)) > maxOutputUTF16 {
			return nil, fmt.Errorf("render[%d] template exceeds UTF-8 or UTF-16 limit: %s", i, mapping.Template)
		}
		dataBytes, err := readBoundedRegular(dataPath, maxDataBytes, "render data")
		if err != nil {
			return nil, err
		}
		var variables map[string]any
		if err := json.Unmarshal(dataBytes, &variables); err != nil {
			return nil, fmt.Errorf("render[%d] data is not JSON object: %w", i, err)
		}
		if variables == nil {
			return nil, fmt.Errorf("render[%d] data must be a JSON object", i)
		}
		result, err := renderer.Render(ctx, companion, string(templateBytes), variables)
		if err != nil {
			return nil, fmt.Errorf("render[%d] %s -> %s: %w", i, mapping.Template, mapping.Output, err)
		}
		content := normalizeMarkdown([]byte(result.Output))
		if !utf8.Valid(content) {
			return nil, fmt.Errorf("render[%d] output is not UTF-8: %s", i, mapping.Output)
		}
		if len(content) > maxOutputBytes || utf16Length(string(content)) > maxOutputUTF16 {
			return nil, fmt.Errorf("render[%d] output exceeds size limit: %s", i, mapping.Output)
		}
		rel, err := cleanRelative(fmt.Sprintf("render[%d].output", i), mapping.Output)
		if err != nil {
			return nil, err
		}
		canonical := "/" + filepath.ToSlash(rel)
		if err := validateOKF(canonical, content); err != nil {
			return nil, fmt.Errorf("render[%d] invalid OKF output: %w", i, err)
		}
		out = append(out, rendered{rel: rel, templatePath: mapping.Template, dataPath: mapping.Data, data: content})
	}
	return out, nil
}

func validateOKF(path string, data []byte) error {
	for _, issue := range parse.ValidateOKF(path, string(data)) {
		if issue.Level == "error" {
			return fmt.Errorf("invalid OKF at %s (%s): %s", path, issue.Field, issue.Message)
		}
	}
	return nil
}

func utf16Length(s string) int {
	length := 0
	for _, r := range s {
		if r > 0xffff {
			length += 2
		} else {
			length++
		}
	}
	return length
}

func cloneDocuments(src map[string]document) map[string]document {
	out := make(map[string]document, len(src))
	for path, doc := range src {
		out[path] = doc
	}
	return out
}

func checkCandidateCollisions(docs map[string]document) error {
	seen := map[string]string{}
	for path := range docs {
		key := canonicalPathKey(path)
		if prior, ok := seen[key]; ok && prior != path {
			return fmt.Errorf("document path collision: %q and %q", prior, path)
		}
		seen[key] = path
	}
	return nil
}

func ensureCandidatePath(docs map[string]document, path string) error {
	key := canonicalPathKey(path)
	for existing := range docs {
		if canonicalPathKey(existing) == key && existing != path {
			return fmt.Errorf("render output collides with document path: %s and %s", existing, path)
		}
	}
	return nil
}

func writeDocuments(root string, docs map[string]document) error {
	for _, path := range sortedPaths(docs) {
		doc := docs[path]
		rel := strings.TrimPrefix(filepath.ToSlash(path), "/")
		target := filepath.Join(root, filepath.FromSlash(rel))
		if err := noSymlinkPath(root, target); err != nil {
			return err
		}
		if err := writeFile(target, doc.data, 0o644); err != nil {
			return fmt.Errorf("stage %s: %w", path, err)
		}
	}
	return nil
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	// #nosec G301 -- staged and published document directories intentionally expose static output.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func stageLedger(project string, data []byte) (string, error) {
	state := filepath.Join(project, ".poolboy")
	if err := noSymlinkPath(project, state); err != nil {
		return "", err
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(state, ".generated-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

type publicationSwap struct {
	output   string
	backup   string
	restored bool
}

func swapPublication(stage, output string) (*publicationSwap, error) {
	parent := filepath.Dir(output)
	// #nosec G301 -- publication parent intentionally exposes static output.
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	backup := ""
	if _, err := os.Lstat(output); err == nil {
		tmp, err := os.MkdirTemp(parent, ".poolboy-output-old-*")
		if err != nil {
			return nil, err
		}
		if err := os.Remove(tmp); err != nil {
			return nil, fmt.Errorf("prepare previous publication backup: %w", err)
		}
		if err := os.Rename(output, tmp); err != nil {
			return nil, fmt.Errorf("stage previous publication: %w", err)
		}
		backup = tmp
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.Rename(stage, output); err != nil {
		if backup != "" {
			if restoreErr := os.Rename(backup, output); restoreErr != nil {
				return nil, errors.Join(
					fmt.Errorf("publish output: %w", err),
					fmt.Errorf("restore previous publication: %w", restoreErr),
				)
			}
		}
		return nil, fmt.Errorf("publish output: %w", err)
	}
	return &publicationSwap{output: output, backup: backup}, nil
}

func (s *publicationSwap) restore() error {
	if s.restored {
		return nil
	}
	if err := os.RemoveAll(s.output); err != nil {
		return err
	}
	if s.backup == "" {
		s.restored = true
		return nil
	}
	if err := os.Rename(s.backup, s.output); err != nil {
		return err
	}
	s.backup = ""
	s.restored = true
	return nil
}

func (s *publicationSwap) commit() error {
	if s.backup == "" {
		return nil
	}
	err := os.RemoveAll(s.backup)
	if err == nil {
		s.backup = ""
	}
	return err
}
