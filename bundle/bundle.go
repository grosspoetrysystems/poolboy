// Package bundle locates a Poolboy project and reads its configuration.
//
// A project is rooted at poolboy.toml. Its Markdown corpus may live in a
// separate directory; generated state lives under .poolboy and publication
// output under the configured project-relative output directory.
package bundle

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Render maps a project-relative Knap template and data file to a corpus-
// relative generated Markdown output.
type Render struct {
	Template string `toml:"template"`
	Data     string `toml:"data"`
	Output   string `toml:"output"`
}

// Landing configures the generated publication landing page. Pointer fields
// preserve absence so compiler defaults can distinguish omitted fields from
// explicit empty values.
type Landing struct {
	Title                *string      `toml:"title"`
	Description          *string      `toml:"description"`
	SecondaryDescription *string      `toml:"secondary_description"`
	Prompt               *string      `toml:"prompt"`
	BaseURL              *string      `toml:"base_url"`
	DownloadFilename     *string      `toml:"download_filename"`
	Mark                 *string      `toml:"mark"`
	Logo                 *string      `toml:"logo"`
	SiteDir              *string      `toml:"site_dir"`
	Style                LandingStyle `toml:"style"`
}

// LandingStyle is the small validated CSS surface accepted by [landing.style].
type LandingStyle struct {
	Font         *string `toml:"font"`
	Text         *string `toml:"text"`
	Background   *string `toml:"background"`
	Button       *string `toml:"button"`
	ButtonText   *string `toml:"button_text"`
	BorderRadius *string `toml:"border_radius"`
}

// Bundle is a located Poolboy project. Root is the project directory that holds
// poolboy.toml; Dir is the configured Markdown corpus directory.
type Bundle struct {
	Root    string   // project directory (holds poolboy.toml)
	Dir     string   // absolute corpus directory
	Name    string   // project name
	Output  string   // project-relative publication directory
	Renders []Render // configured template/data/output mappings
	Landing Landing  // optional generated/custom landing page configuration
	Spec    string   // Poolboy spec version
	Types   []string // declared content types; empty means any type
	Ignore  []string // paths relative to Dir that Poolboy disregards
	// IgnoreOrphans lists paths relative to Dir whose entries remain indexed but
	// are omitted from the orphan report.
	IgnoreOrphans []string
	// Unknown holds unrecognized poolboy.toml keys as dotted paths. They are
	// inert for the maintenance engine and surfaced by check; build and scan
	// reject them.
	Unknown []string

	// The [tool.<name>] tables are intentionally opaque. DecodeTool is the
	// stable boundary for other tools sharing this project configuration.
	tool map[string]toml.Primitive
	md   toml.MetaData
}

// Tools lists the names of the [tool.<name>] tables present, sorted.
func (b *Bundle) Tools() []string {
	return slices.Sorted(maps.Keys(b.tool))
}

// DecodeTool unmarshals the [tool.<name>] table into v, which is any struct or
// map the caller defines. Reports whether the table was present.
func (b *Bundle) DecodeTool(name string, v any) (bool, error) {
	p, ok := b.tool[name]
	if !ok {
		return false, nil
	}
	if err := b.md.PrimitiveDecode(p, v); err != nil {
		return true, fmt.Errorf("poolboy.toml [tool.%s]: %w", name, err)
	}
	return true, nil
}

// ErrNotFound is returned when no poolboy.toml is found walking up from start.
var ErrNotFound = errors.New("no poolboy.toml found (not inside a Poolboy project)")

// Discover walks up from start until it finds a directory containing
// poolboy.toml. The returned paths are canonical absolute paths.
func Discover(start string) (*Bundle, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	if fi, statErr := os.Stat(dir); statErr == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		cfg := filepath.Join(dir, "poolboy.toml")
		if fi, statErr := os.Lstat(cfg); statErr == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("poolboy.toml must not be a symlink: %s", cfg)
			}
			if fi.IsDir() {
				return nil, fmt.Errorf("poolboy.toml is a directory: %s", cfg)
			}
			return load(dir, cfg)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, ErrNotFound
		}
		dir = parent
	}
}

// config is the subset of poolboy.toml interpreted by the core. [tool.*] is
// deliberately opaque and all other undecoded keys are reported in Bundle.Unknown.
type config struct {
	Spec          string                    `toml:"spec"`
	Name          string                    `toml:"name"`
	Corpus        string                    `toml:"corpus"`
	Output        string                    `toml:"output"`
	Renders       []Render                  `toml:"render"`
	Types         []string                  `toml:"types"`
	Ignore        []string                  `toml:"ignore"`
	IgnoreOrphans []string                  `toml:"ignore_orphans"`
	Tool          map[string]toml.Primitive `toml:"tool"`
}

// loadLanding reads the optional landing.toml sitting beside poolboy.toml. A
// missing file is not an error: the compiler applies built-in defaults. Its
// keys are the [landing] fields hoisted to the top level, with [style] for the
// former [landing.style]. Unrecognized keys are returned for the same
// typo-surfacing path as poolboy.toml.
func loadLanding(dir string) (Landing, []string, error) {
	var l Landing
	md, err := toml.DecodeFile(filepath.Join(dir, "landing.toml"), &l)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Landing{}, nil, nil
		}
		return Landing{}, nil, fmt.Errorf("landing.toml: %w", err)
	}
	var unknown []string
	for _, k := range md.Undecoded() {
		unknown = append(unknown, "landing.toml:"+k.String())
	}
	return l, unknown, nil
}

func load(projectRoot, cfgPath string) (*Bundle, error) {
	root, err := canonicalDir(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("project root: %w", err)
	}
	var c config
	md, err := toml.DecodeFile(cfgPath, &c)
	if err != nil {
		return nil, fmt.Errorf("poolboy.toml: %w", err)
	}

	unknown := undecoded(md)
	corpus := c.Corpus
	if corpus == "" {
		corpus = "."
	}
	corpus, err = projectPath("corpus", corpus)
	if err != nil {
		return nil, err
	}
	corpusDir := filepath.Join(root, filepath.FromSlash(corpus))
	if err := safeExistingDir(root, corpusDir, "corpus"); err != nil {
		return nil, err
	}

	output := c.Output
	if output == "" {
		output = "dist"
	}
	output, err = projectPath("output", output)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(output, "/") {
		switch part {
		case ".git", ".substrate", ".poolboy":
			return nil, fmt.Errorf("output uses reserved project directory %q", part)
		}
	}
	outputDir := filepath.Join(root, filepath.FromSlash(output))
	if err := safePath(root, outputDir, "output"); err != nil {
		return nil, err
	}
	if fi, statErr := os.Stat(outputDir); statErr == nil && !fi.IsDir() {
		return nil, fmt.Errorf("output is not a directory: %s", output)
	}
	// An output root may be below the corpus (the portable default is corpus=.
	// and output=dist), but it may not contain or equal the authoring root.
	if sameOrWithin(outputDir, corpusDir) {
		return nil, fmt.Errorf("output overlaps corpus: output %q, corpus %q", output, corpus)
	}

	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = filepath.Base(root)
	}
	if strings.ContainsAny(name, `/\\`) || name == "." || name == ".." {
		return nil, fmt.Errorf("name must be a plain project name: %q", name)
	}

	renders := make([]Render, len(c.Renders))
	copy(renders, c.Renders)
	for i := range renders {
		r := &renders[i]
		r.Template, err = projectPath(fmt.Sprintf("render[%d].template", i), r.Template)
		if err != nil {
			return nil, err
		}
		r.Data, err = projectPath(fmt.Sprintf("render[%d].data", i), r.Data)
		if err != nil {
			return nil, err
		}
		r.Output, err = relativePath(fmt.Sprintf("render[%d].output", i), r.Output)
		if err != nil {
			return nil, err
		}
		if r.Template == "." || r.Data == "." || r.Output == "." {
			return nil, fmt.Errorf("render[%d] paths must name files", i)
		}
		for label, rel := range map[string]string{
			"template": r.Template,
			"data":     r.Data,
		} {
			if err := safePath(root, filepath.Join(root, filepath.FromSlash(rel)), "render "+label); err != nil {
				return nil, err
			}
		}
		if err := safePath(corpusDir, filepath.Join(corpusDir, filepath.FromSlash(r.Output)), "render output"); err != nil {
			return nil, err
		}
	}

	landing, landingUnknown, err := loadLanding(filepath.Dir(cfgPath))
	if err != nil {
		return nil, err
	}
	unknown = append(unknown, landingUnknown...)
	if landing.Logo != nil && strings.TrimSpace(*landing.Logo) != "" {
		p, err := projectPath("landing.logo", *landing.Logo)
		if err != nil {
			return nil, err
		}
		landing.Logo = &p
	}
	if landing.SiteDir != nil && strings.TrimSpace(*landing.SiteDir) != "" {
		p, err := projectPath("landing.site_dir", *landing.SiteDir)
		if err != nil {
			return nil, err
		}
		landing.SiteDir = &p
	}

	spec := strings.TrimSpace(c.Spec)
	if spec == "" {
		spec = "0.2"
	}
	return &Bundle{
		Root:          root,
		Dir:           corpusDir,
		Name:          name,
		Output:        output,
		Renders:       renders,
		Landing:       landing,
		Spec:          spec,
		Types:         c.Types,
		Ignore:        c.Ignore,
		IgnoreOrphans: c.IgnoreOrphans,
		tool:          c.Tool,
		Unknown:       unknown,
		md:            md,
	}, nil
}

func undecoded(md toml.MetaData) []string {
	var unknown []string
	for _, k := range md.Undecoded() {
		if len(k) > 0 && k[0] == "tool" {
			continue
		}
		s := k.String()
		if slices.ContainsFunc(unknown, func(u string) bool { return strings.HasPrefix(s, u+".") }) {
			continue
		}
		unknown = append(unknown, s)
	}
	return unknown
}

// projectPath validates a project-relative setting and normalizes it to slash
// separators. Empty values are rejected by callers that require a file path.
func projectPath(label, value string) (string, error) {
	return relativePath(label, value)
}

func relativePath(label, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", label)
	}
	if strings.IndexByte(value, 0) >= 0 || filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\\`) {
		return "", fmt.Errorf("%s must be project-relative: %q", label, value)
	}
	// Treat both slash styles as separators for portability; a backslash is not
	// a safe filename in a cross-platform project configuration.
	portable := strings.ReplaceAll(value, `\`, "/")
	clean := path.Clean(portable)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%s escapes the project: %q", label, value)
	}
	return clean, nil
}

// NormalizeResource applies the OKF resource spelling rules shared by source
// inventory and maintenance: trim whitespace/quotes and use slash separators.
func NormalizeResource(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 {
		if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
			raw = raw[1 : len(raw)-1]
		}
	}
	raw = strings.TrimSpace(raw)
	return strings.ReplaceAll(raw, "\\", "/")
}

// ResolveResourcePath resolves an OKF sources.resource using the existing
// semantics: explicit ./ and ../ values are relative to the declaring document;
// other relative values and root-leading / values are project-root relative.
func ResolveResourcePath(root, document, raw string) (string, error) {
	raw = NormalizeResource(raw)
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, root+string(filepath.Separator)) && raw != root {
		return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(raw, "/"))), nil
	}
	switch {
	case filepath.IsAbs(raw):
		return filepath.Clean(filepath.FromSlash(raw)), nil
	case strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../"):
		return filepath.Clean(filepath.Join(filepath.Dir(document), filepath.FromSlash(raw))), nil
	default:
		return filepath.Clean(filepath.Join(root, filepath.FromSlash(raw))), nil
	}
}

func canonicalDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("not a directory: %s", dir)
	}
	return filepath.Clean(resolved), nil
}

func safeExistingDir(root, p, label string) error {
	if err := safePath(root, p, label); err != nil {
		return err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("%s directory does not exist: %s", label, p)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory: %s", label, p)
	}
	return nil
}

// safePath rejects symlink components. It allows a not-yet-created leaf (such
// as dist) while still checking every existing parent component.
func safePath(root, p, label string) error {
	if !within(root, p) {
		return fmt.Errorf("%s escapes project root: %s", label, p)
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return fmt.Errorf("%s path: %w", label, err)
	}
	cur := root
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			cur = filepath.Join(cur, part)
			fi, statErr := os.Lstat(cur)
			if statErr != nil {
				if os.IsNotExist(statErr) {
					break
				}
				return fmt.Errorf("%s: %w", label, statErr)
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%s must not contain symlink %s", label, cur)
			}
		}
	}
	return nil
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sameOrWithin(root, p string) bool {
	return within(root, p)
}

// KnownType reports whether t is an allowed content type. A declared
// vocabulary (`types` in poolboy.toml) is opt-in; when none is declared, every
// type is allowed.
func (b *Bundle) KnownType(t string) bool {
	return len(b.Types) == 0 || slices.Contains(b.Types, t)
}

// okfVersions maps Poolboy spec versions to the OKF version embedded in the
// bundle-root index.md.
var okfVersions = map[string]string{"0.1": "0.1", "0.2": "0.2"}

// OKFVersion returns the OKF version this bundle's spec embeds, and whether the
// spec embeds OKF at all.
func (b *Bundle) OKFVersion() (string, bool) {
	v, ok := okfVersions[b.Spec]
	return v, ok
}
