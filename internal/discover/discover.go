// Package discover finds independently published Poolboy corpora from explicit
// links. Discovery is deliberately opt-in: callers provide seed URLs and the
// engine follows only bounded, ordinary page/Markdown links before validating
// a Poolboy graph publication.
package discover

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	pathpkg "path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxDepth             = 3
	defaultMaxReferences        = 32
	defaultMaxFetches           = 64
	defaultRequestTimeout       = 10 * time.Second
	defaultTotalTimeout         = 30 * time.Second
	defaultResponseBytes        = 2 << 20
	maxPublicationManifestBytes = 32 << 20
	defaultGraphBytes           = maxPublicationManifestBytes
	defaultLLMSBytes            = 2 << 20
	defaultAggregateBytes       = 64 << 20
	defaultMaxRedirects         = 5
	maxDiagnostics              = 128
	maxPageLinks                = 256
	maxGraphExternalReferences  = 32
	maxGraphFiles               = 100_000
	maxGraphFileBytes           = 8 << 20
	maxGraphCorpusBytes         = 64 << 20
	maxGraphLLMSBytes           = 4 << 20
)

// Candidate is an explicit URL and its local declaring provenance. From is a
// project-relative or corpus path; Line is one-based. Origin is "corpus" or
// "source" for Poolboy-generated references, and remains untrusted metadata
// when it comes from a remote graph.
type Candidate struct {
	URL    string
	From   string
	Line   int
	Origin string
}

// ExternalReference is the additive graph-v0 representation of a validated
// remote publication. The graph digest is over the fetched graph.json bytes;
// it is an identity hint, not a publisher or content trust assertion.
type ExternalReference struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Root   string `json:"root"`
	From   string `json:"from"`
	Line   int    `json:"line"`
	Origin string `json:"origin"`
}

// Diagnostic reports a bounded, non-fatal discovery observation. A failed
// ordinary external link does not make local publication fail.
type Diagnostic struct {
	URL    string `json:"url"`
	Detail string `json:"detail"`
}

// Options bounds all network and traversal work. Zero values use conservative
// defaults. These limits are intentionally not configurable by Poolboy's
// product configuration: --discover is an explicit, bounded operation.
type Options struct {
	MaxDepth       int
	MaxReferences  int
	MaxFetches     int
	RequestTimeout time.Duration
	TotalTimeout   time.Duration
	ResponseBytes  int64
	GraphBytes     int64
	LLMSBytes      int64
	AggregateBytes int64
	MaxRedirects   int
}

type graphArtifact struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type graphFile struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type childReference struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Root   string `json:"root"`
	From   string `json:"from"`
	Line   int    `json:"line"`
	Origin string `json:"origin"`
}

type graphManifest struct {
	Version            string               `json:"version"`
	Root               string               `json:"root"`
	LLMS               graphArtifact        `json:"llms"`
	Files              map[string]graphFile `json:"files"`
	ExternalReferences []childReference     `json:"external_references,omitempty"`
}

type crawlScope struct {
	scheme       string
	host         string
	prefix       string
	allowForeign bool
}

type queuedCandidate struct {
	candidate    Candidate
	depth        int
	expectedHash string
	scope        *crawlScope
	seed         int
}
type cachedFetch struct {
	final string
	body  []byte
}

var errDiscoveryBudget = errors.New("discovery budget exhausted")

type engine struct {
	options        Options
	client         *http.Client
	queue          []queuedCandidate
	seenCandidates map[string]bool
	seenFetches    map[string]bool
	responses      map[string]cachedFetch
	seenGraphs     map[string]bool
	references     map[string]ExternalReference
	diagnostics    []Diagnostic
	fetches        int
	aggregate      int64
	budgetStopped  bool
	budgetReason   string
	frontierNoted  bool
	published      []crawlScope
	seedCount      int
	nextSeed       int
}

// Discover validates publications reachable from explicit candidates. It
// never returns a reference for an unvalidated graph, and it never returns an
// error for an ordinary missing/unreachable external link; such observations
// are diagnostics. Context cancellation is returned as an error because it is
// an operator-requested stop rather than an ordinary remote failure.
func Discover(ctx context.Context, candidates []Candidate, options Options) ([]ExternalReference, []Diagnostic, error) {
	return discoverWithClient(ctx, candidates, options, nil)
}

func discoverWithClient(ctx context.Context, candidates []Candidate, options Options, client *http.Client) ([]ExternalReference, []Diagnostic, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	o := withDefaults(options)
	parentCtx := ctx
	runCtx, cancel := context.WithTimeout(parentCtx, o.TotalTimeout)
	defer cancel()
	e := newEngine(o)
	if client != nil {
		e.client = client
	}
	seeds := append([]Candidate(nil), candidates...)
	sort.SliceStable(seeds, func(i, j int) bool { return candidateLess(seeds[i], seeds[j]) })
	e.seedCount = len(seeds)
	for seed, candidate := range seeds {
		e.queue = append(e.queue, queuedCandidate{candidate: candidate, scope: seedScope(candidate.URL), seed: seed})
	}
	for len(e.queue) > 0 && !e.budgetStopped {
		if err := parentCtx.Err(); err != nil {
			return e.sortedReferences(), e.diagnostics, err
		}
		if err := runCtx.Err(); err != nil {
			break
		}
		item, ok := e.popCandidate()
		if !ok {
			break
		}
		e.visit(runCtx, item)
	}
	if err := parentCtx.Err(); err != nil {
		return e.sortedReferences(), e.diagnostics, err
	}
	if err := runCtx.Err(); err != nil {
		e.note("", "discovery time budget reached; returning partial results")
	}
	if e.budgetStopped {
		e.note("", fmt.Sprintf("discovery stopped: %s; returning partial results", e.budgetReason))
	}
	return e.sortedReferences(), e.diagnostics, nil
}

func (e *engine) popCandidate() (queuedCandidate, bool) {
	if len(e.queue) == 0 {
		return queuedCandidate{}, false
	}
	if e.seedCount <= 1 {
		item := e.queue[0]
		e.queue = e.queue[1:]
		return item, true
	}
	// ponytail: one frontier scan per turn; use per-seed FIFOs if scheduling consumes the crawl budget.
	next := 0
	distance := e.seedCount
	for i, item := range e.queue {
		offset := item.seed - e.nextSeed
		if offset < 0 {
			offset += e.seedCount
		}
		if offset < distance {
			next, distance = i, offset
			if distance == 0 {
				break
			}
		}
	}
	item := e.queue[next]
	copy(e.queue[next:], e.queue[next+1:])
	last := len(e.queue) - 1
	e.queue[last] = queuedCandidate{}
	e.queue = e.queue[:last]
	e.nextSeed = (item.seed + 1) % e.seedCount
	return item, true
}

func withDefaults(o Options) Options {
	if o.MaxDepth <= 0 {
		o.MaxDepth = defaultMaxDepth
	}
	if o.MaxReferences <= 0 {
		o.MaxReferences = defaultMaxReferences
	}
	if o.MaxFetches <= 0 {
		o.MaxFetches = defaultMaxFetches
	}
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = defaultRequestTimeout
	}
	if o.TotalTimeout <= 0 {
		o.TotalTimeout = defaultTotalTimeout
	}
	if o.ResponseBytes <= 0 {
		o.ResponseBytes = defaultResponseBytes
	}
	if o.GraphBytes <= 0 {
		o.GraphBytes = defaultGraphBytes
	}
	if o.LLMSBytes <= 0 {
		o.LLMSBytes = defaultLLMSBytes
	}
	if o.AggregateBytes <= 0 {
		o.AggregateBytes = defaultAggregateBytes
	}
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = defaultMaxRedirects
	}
	return o
}

func newEngine(o Options) *engine {
	resolver := &net.Resolver{}
	dialer := &safeDialer{resolver: resolver, dialer: &net.Dialer{Timeout: o.RequestTimeout}, pins: make(map[string][]net.IP)}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       10 * time.Second,
		TLSHandshakeTimeout:   o.RequestTimeout,
		ResponseHeaderTimeout: o.RequestTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > o.MaxRedirects {
			return fmt.Errorf("redirect limit exceeded (%d)", o.MaxRedirects)
		}
		if _, err := validateURL(req.URL.String()); err != nil {
			return err
		}
		return nil
	}}
	return &engine{
		options:        o,
		client:         client,
		seenCandidates: make(map[string]bool),
		seenFetches:    make(map[string]bool),
		responses:      make(map[string]cachedFetch),
		seenGraphs:     make(map[string]bool),
		references:     make(map[string]ExternalReference),
	}
}

func (e *engine) visit(ctx context.Context, item queuedCandidate) {
	if item.depth > e.options.MaxDepth {
		e.note(item.candidate.URL, "discovery depth limit reached")
		return
	}
	u, err := validateURL(item.candidate.URL)
	if err != nil {
		e.note(item.candidate.URL, err.Error())
		return
	}
	// A graph URL may be reached unpinned and under different pins; each distinct
	// pin is checked against the cached response rather than hidden by the URL.
	key := canonicalURL(u) + "\x00" + item.expectedHash
	if e.seenCandidates[key] {
		return
	}
	e.seenCandidates[key] = true
	path := strings.ToLower(u.Path)
	switch {
	case strings.HasSuffix(path, "/graph.json") || path == "graph.json":
		e.tryGraph(ctx, u, item.candidate, item.depth, item.expectedHash, item.seed)
	case strings.HasSuffix(path, "/llms.txt") || path == "llms.txt":
		e.tryLLMS(ctx, u, item.candidate, item.depth, item.seed)
	default:
		if e.isPublishedPage(u) {
			return
		}
		e.tryPage(ctx, u, item.candidate, item.depth, item.scope, item.seed)
	}
}

func (e *engine) tryPage(ctx context.Context, u *url.URL, provenance Candidate, depth int, scope *crawlScope, seed int) {
	finalURL, body, err := e.fetch(ctx, u, e.options.ResponseBytes)
	if err != nil {
		if !errors.Is(err, errDiscoveryBudget) {
			e.note(u.String(), err.Error())
		}
		return
	}
	final, err := validateURL(finalURL)
	if err != nil {
		e.note(u.String(), err.Error())
		return
	}
	links, truncated := extractLinks(body, final)
	if truncated {
		e.note(final.String(), "page link budget reached; some links were not followed")
	}
	for _, link := range links {
		e.queuePageLink(scope, link, provenance, depth+1, seed)
	}
}

func (e *engine) tryLLMS(ctx context.Context, u *url.URL, provenance Candidate, depth, seed int) {
	finalURL, body, err := e.fetch(ctx, u, e.options.LLMSBytes)
	if err != nil {
		if !errors.Is(err, errDiscoveryBudget) {
			e.note(u.String(), err.Error())
		}
		return
	}
	final, err := validateURL(finalURL)
	if err != nil {
		e.note(u.String(), err.Error())
		return
	}
	links, truncated := extractLinks(body, final)
	if truncated {
		e.note(final.String(), "llms link budget reached; some links were not followed")
	}
	for _, link := range links {
		target, parseErr := validateURL(link)
		if parseErr != nil || !isGraphURL(target) {
			continue
		}
		e.queue = append(e.queue, queuedCandidate{
			candidate: Candidate{URL: canonicalURL(target), From: provenance.From, Line: provenance.Line, Origin: provenance.Origin},
			depth:     depth, seed: seed,
		})
	}
}

func (e *engine) tryGraph(ctx context.Context, u *url.URL, provenance Candidate, depth int, expectedHash string, seed int) {
	graphLimit := e.options.GraphBytes
	if graphLimit > maxPublicationManifestBytes {
		graphLimit = maxPublicationManifestBytes
	}
	finalURL, body, err := e.fetch(ctx, u, graphLimit)
	if err != nil {
		if !errors.Is(err, errDiscoveryBudget) {
			e.note(u.String(), err.Error())
		}
		return
	}
	final, err := validateURL(finalURL)
	if err != nil {
		e.note(u.String(), err.Error())
		return
	}
	if final.RawQuery != "" || final.RawPath != "" || final.Path != pathpkg.Clean(final.Path) || !isGraphURL(final) {
		e.note(final.String(), "graph publication URL is not canonical")
		return
	}
	if expectedHash != "" && !strings.EqualFold(expectedHash, digest(body)) {
		e.note(final.String(), "external reference graph hash does not match fetched graph")
		return
	}
	key := canonicalURL(final)
	if e.seenGraphs[key] {
		return
	}
	manifest, err := decodeManifest(body)
	if err != nil {
		e.note(final.String(), err.Error())
		return
	}
	if err := e.verifyManifest(ctx, final, manifest); err != nil {
		if !errors.Is(err, errDiscoveryBudget) {
			e.note(final.String(), err.Error())
		}
		return
	}
	if _, exists := e.references[key]; !exists && len(e.references) >= e.options.MaxReferences {
		_ = e.stopBudget(fmt.Sprintf("reference budget reached (%d)", e.options.MaxReferences))
		return
	}
	e.seenGraphs[key] = true
	reference := ExternalReference{
		URL: key, SHA256: digest(body), Root: manifest.Root,
		From: provenance.From, Line: provenance.Line, Origin: provenance.Origin,
	}
	if prior, ok := e.references[key]; !ok || referenceLess(reference, prior) {
		e.references[key] = reference
	}
	e.markPublished(final)
	children := append([]childReference(nil), manifest.ExternalReferences...)
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].URL != children[j].URL {
			return children[i].URL < children[j].URL
		}
		if children[i].From != children[j].From {
			return children[i].From < children[j].From
		}
		return children[i].Line < children[j].Line
	})
	for _, child := range children {
		childURL, parseErr := validateURL(child.URL)
		childPath := ""
		if childURL != nil {
			childPath = strings.ToLower(childURL.Path)
		}
		if parseErr != nil || childURL == nil || childURL.RawQuery != "" || childURL.RawPath != "" || childURL.Path != pathpkg.Clean(childURL.Path) || !strings.HasSuffix(childPath, "/graph.json") {
			e.note(final.String(), "external reference does not target a graph.json publication")
			continue
		}
		e.queue = append(e.queue, queuedCandidate{
			candidate: Candidate{URL: canonicalURL(childURL), From: provenance.From, Line: provenance.Line, Origin: provenance.Origin},
			depth:     depth + 1, expectedHash: child.SHA256, seed: seed,
		})
	}
}

func seedScope(raw string) *crawlScope {
	u, err := validateURL(raw)
	if err != nil {
		return nil
	}
	return &crawlScope{scheme: strings.ToLower(u.Scheme), host: strings.ToLower(u.Host), prefix: "/", allowForeign: true}
}

func entryScope(u *url.URL) *crawlScope {
	prefix := "/"
	if u != nil && u.Path != "" {
		prefix = strings.TrimSuffix(u.Path, "/")
		if prefix == "" {
			prefix = "/"
		}
	}
	return &crawlScope{scheme: strings.ToLower(u.Scheme), host: strings.ToLower(u.Host), prefix: prefix}
}

func scopeAllows(scope *crawlScope, u *url.URL) bool {
	if scope == nil || u == nil || strings.ToLower(u.Scheme) != scope.scheme || strings.ToLower(u.Host) != scope.host {
		return false
	}
	if scope.prefix == "/" || u.Path == scope.prefix {
		return true
	}
	return strings.HasPrefix(u.Path, scope.prefix+"/")
}

func isGraphURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	path := strings.ToLower(u.Path)
	return path == "graph.json" || strings.HasSuffix(path, "/graph.json")
}

func isLLMSURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	path := strings.ToLower(u.Path)
	return path == "llms.txt" || strings.HasSuffix(path, "/llms.txt")
}

func (e *engine) queuePageLink(scope *crawlScope, raw string, provenance Candidate, depth, seed int) {
	target, err := validateURL(raw)
	if err != nil || !relevantLink(raw) {
		return
	}
	if isGraphURL(target) || isLLMSURL(target) {
		e.queue = append(e.queue, queuedCandidate{
			candidate: Candidate{URL: canonicalURL(target), From: provenance.From, Line: provenance.Line, Origin: provenance.Origin},
			depth:     depth, scope: scope, seed: seed,
		})
		return
	}
	if scopeAllows(scope, target) {
		e.queue = append(e.queue, queuedCandidate{
			candidate: Candidate{URL: canonicalURL(target), From: provenance.From, Line: provenance.Line, Origin: provenance.Origin},
			depth:     depth, scope: scope, seed: seed,
		})
		return
	}
	if scope != nil && scope.allowForeign {
		e.queue = append(e.queue, queuedCandidate{
			candidate: Candidate{URL: canonicalURL(target), From: provenance.From, Line: provenance.Line, Origin: provenance.Origin},
			depth:     depth, scope: entryScope(target), seed: seed,
		})
		return
	}
	if !e.frontierNoted {
		e.frontierNoted = true
		e.note(raw, "ordinary page link is outside discovery frontier; some links were not followed")
	}
}

func (e *engine) isPublishedPage(u *url.URL) bool {
	for i := range e.published {
		scope := &e.published[i]
		if scopeAllows(scope, u) {
			return true
		}
	}
	return false
}

func (e *engine) markPublished(graphURL *url.URL) {
	scope := entryScope(publicationBase(graphURL))
	for i := range e.published {
		if e.published[i].scheme == scope.scheme && e.published[i].host == scope.host && e.published[i].prefix == scope.prefix {
			return
		}
	}
	e.published = append(e.published, *scope)
}
func (e *engine) verifyManifest(ctx context.Context, graphURL *url.URL, manifest *graphManifest) error {
	if manifest.LLMS.Bytes <= 0 || manifest.LLMS.Bytes > maxGraphLLMSBytes {
		return fmt.Errorf("graph has invalid llms.txt byte declaration")
	}
	if !validHash(manifest.LLMS.SHA256) {
		return fmt.Errorf("graph has invalid llms.txt hash")
	}
	base := publicationBase(graphURL)
	llmsURL := base.JoinPath("llms.txt")
	_, llms, err := e.fetch(ctx, llmsURL, e.options.LLMSBytes)
	if err != nil {
		return fmt.Errorf("fetch llms.txt: %w", err)
	}
	if len(llms) != manifest.LLMS.Bytes || digest(llms) != manifest.LLMS.SHA256 {
		return errors.New("graph llms.txt declaration does not match fetched llms.txt")
	}
	root, ok := manifest.Files[manifest.Root]
	if !ok {
		return errors.New("graph is missing declared root file")
	}
	rootURL := base.JoinPath(strings.TrimPrefix(manifest.Root, "/"))
	_, rootBody, err := e.fetch(ctx, rootURL, maxGraphFileBytes)
	if err != nil {
		return fmt.Errorf("fetch graph root: %w", err)
	}
	if len(rootBody) != root.Bytes || digest(rootBody) != root.SHA256 {
		return errors.New("graph root declaration does not match fetched root")
	}
	return nil
}

func decodeManifest(body []byte) (*graphManifest, error) {
	var manifest graphManifest
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("invalid graph.json: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("graph.json contains multiple JSON values")
		}
		return nil, fmt.Errorf("graph.json has trailing data: %w", err)
	}
	if manifest.Version != "0" || manifest.Root != "/index.md" || len(manifest.Files) == 0 {
		return nil, errors.New("graph.json is not a Poolboy v0 publication")
	}
	if len(manifest.Files) > maxGraphFiles {
		return nil, errors.New("graph.json declares too many files")
	}
	var total int64
	for name, file := range manifest.Files {
		if !canonicalMarkdownPath(name) {
			return nil, fmt.Errorf("graph.json has non-canonical file path %q", name)
		}
		if file.Bytes <= 0 || file.Bytes > maxGraphFileBytes || !validHash(file.SHA256) {
			return nil, fmt.Errorf("graph.json has invalid file declaration %q", name)
		}
		total += int64(file.Bytes)
		if total > maxGraphCorpusBytes {
			return nil, errors.New("graph.json declares too many corpus bytes")
		}
	}
	if len(manifest.ExternalReferences) > maxGraphExternalReferences {
		return nil, fmt.Errorf("graph.json declares too many external references")
	}
	for _, ref := range manifest.ExternalReferences {
		if strings.TrimSpace(ref.URL) == "" || ref.Root != "/index.md" || !validHash(ref.SHA256) {
			return nil, errors.New("graph.json has an invalid external reference")
		}
		if ref.Line < 0 {
			return nil, errors.New("graph.json has an invalid external reference")
		}
	}
	return &manifest, nil
}

func canonicalMarkdownPath(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.ContainsAny(value, `\\#?`) && value == pathpkg.Clean(value) && strings.HasSuffix(strings.ToLower(value), ".md") && value != "/"
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (e *engine) stopBudget(reason string) error {
	if !e.budgetStopped {
		e.budgetStopped = true
		e.budgetReason = reason
	}
	return errDiscoveryBudget
}

func (e *engine) fetch(ctx context.Context, raw *url.URL, limit int64) (string, []byte, error) {
	if raw == nil {
		return "", nil, errors.New("nil URL")
	}
	u, err := validateURL(raw.String())
	if err != nil {
		return "", nil, err
	}
	key := canonicalURL(u)
	if cached, ok := e.responses[key]; ok {
		if limit > 0 && int64(len(cached.body)) > limit {
			return cached.final, nil, fmt.Errorf("response exceeds %d-byte limit", limit)
		}
		return cached.final, cached.body, nil
	}
	if e.seenFetches[key] {
		return "", nil, fmt.Errorf("URL already fetched: %s", key)
	}
	if e.fetches >= e.options.MaxFetches {
		return "", nil, e.stopBudget(fmt.Sprintf("fetch budget exhausted (%d)", e.options.MaxFetches))
	}
	e.seenFetches[key] = true
	e.fetches++
	requestCtx, cancel := context.WithTimeout(ctx, e.options.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, key, nil)
	if err != nil {
		return "", nil, err
	}
	response, err := e.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = response.Body.Close() }()
	final := key
	if response.Request != nil && response.Request.URL != nil {
		if _, err := validateURL(response.Request.URL.String()); err != nil {
			return "", nil, fmt.Errorf("unsafe redirect destination: %w", err)
		}
		final = canonicalURL(response.Request.URL)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return final, nil, fmt.Errorf("HTTP status %s", response.Status)
	}
	if limit <= 0 {
		limit = e.options.ResponseBytes
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return final, nil, err
	}
	if int64(len(body)) > limit {
		return final, nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	e.aggregate += int64(len(body))
	if e.aggregate > e.options.AggregateBytes {
		return final, nil, e.stopBudget(fmt.Sprintf("aggregate byte budget exhausted (%d bytes)", e.options.AggregateBytes))
	}
	e.responses[key] = cachedFetch{final: final, body: body}
	return final, body, nil
}

func validateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, errors.New("discovery requires HTTPS URLs")
	}
	if u.Host == "" || u.Hostname() == "" {
		return nil, errors.New("discovery URL has no host")
	}
	if u.User != nil {
		return nil, errors.New("discovery URL credentials are not allowed")
	}
	if u.Fragment != "" {
		return nil, errors.New("discovery URL fragments are not allowed")
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, fmt.Errorf("invalid discovery URL port %q", port)
		}
	}
	return u, nil
}

func canonicalURL(u *url.URL) string {
	normalized := *u
	normalized.Scheme = strings.ToLower(normalized.Scheme)
	normalized.Host = strings.ToLower(normalized.Host)
	normalized.Fragment = ""
	if normalized.Path == "" {
		normalized.Path = "/"
	}
	return normalized.String()
}

func publicationBase(u *url.URL) *url.URL {
	base := *u
	base.RawQuery = ""
	base.Fragment = ""
	if !strings.HasSuffix(base.Path, "/") {
		dir := pathpkg.Dir(base.Path)
		if dir == "/" || dir == "." {
			base.Path = "/"
		} else {
			base.Path = dir + "/"
		}
	}
	if base.Path == "./" || base.Path == "" {
		base.Path = "/"
	}
	return &base
}

var (
	absoluteURLRe        = regexp.MustCompile(`https?://[^\s"'<>]+`)
	markdownURLRe        = regexp.MustCompile(`\]\(\s*<?([^\s)>]+)`)
	markdownDefinitionRe = regexp.MustCompile(`(?m)^\s{0,3}\[[^\]]+\]:\s*<?([^\s>]+)`)
	anchorHrefRe         = regexp.MustCompile(`(?is)<a\b[^>]*\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	scriptBlockRe        = regexp.MustCompile(`(?is)<(?:script|style|template)\b[^>]*>.*?</(?:script|style|template)\s*>`)
	tagRe                = regexp.MustCompile(`(?is)<[^>]*>`)
)

func extractLinks(body []byte, base *url.URL) ([]string, bool) {
	seen := make(map[string]bool)
	links := make([]string, 0, maxPageLinks)
	truncated := false
	add := func(raw string) {
		raw = html.UnescapeString(strings.TrimSpace(strings.Trim(raw, "<>\"'")))
		raw = strings.TrimRight(raw, ".,;:!?]")
		if raw == "" || strings.HasPrefix(raw, "#") {
			return
		}
		resolved, err := url.Parse(raw)
		if err != nil || base == nil {
			return
		}
		resolved = base.ResolveReference(resolved)
		if resolved.Scheme != "https" || resolved.Host == "" {
			return
		}
		resolved.Fragment = ""
		key := canonicalURL(resolved)
		if seen[key] {
			return
		}
		if len(links) >= maxPageLinks {
			truncated = true
			return
		}
		seen[key] = true
		links = append(links, key)
	}
	text := scriptBlockRe.ReplaceAllString(string(body), " ")
	for _, match := range anchorHrefRe.FindAllStringSubmatch(text, -1) {
		for _, raw := range match[1:] {
			if raw != "" {
				add(raw)
				break
			}
		}
	}
	for _, match := range absoluteURLRe.FindAllString(tagRe.ReplaceAllString(text, " "), -1) {
		add(match)
	}
	for _, match := range markdownURLRe.FindAllStringSubmatch(text, -1) {
		add(match[1])
	}
	for _, match := range markdownDefinitionRe.FindAllStringSubmatch(text, -1) {
		add(match[1])
	}
	return links, truncated
}

func relevantLink(raw string) bool {
	u, err := validateURL(raw)
	if err != nil {
		return false
	}
	path := strings.ToLower(u.Path)
	if strings.HasSuffix(path, "/graph.json") || strings.HasSuffix(path, "/llms.txt") || strings.HasSuffix(path, "/") {
		return true
	}
	ext := pathpkg.Ext(path)
	return ext == "" || ext == ".md" || ext == ".markdown" || ext == ".html" || ext == ".htm" || ext == ".json"
}

func candidateLess(a, b Candidate) bool {
	if a.From != b.From {
		return a.From < b.From
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.URL != b.URL {
		return a.URL < b.URL
	}
	return a.Origin < b.Origin
}

func referenceLess(a, b ExternalReference) bool {
	if a.From != b.From {
		return a.From < b.From
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.URL != b.URL {
		return a.URL < b.URL
	}
	return a.Origin < b.Origin
}

func (e *engine) sortedReferences() []ExternalReference {
	out := make([]ExternalReference, 0, len(e.references))
	for _, reference := range e.references {
		out = append(out, reference)
	}
	sort.Slice(out, func(i, j int) bool { return referenceLess(out[i], out[j]) })
	return out
}

func (e *engine) note(raw, detail string) {
	if len(e.diagnostics) >= maxDiagnostics {
		return
	}
	e.diagnostics = append(e.diagnostics, Diagnostic{URL: raw, Detail: detail})
}

type safeDialer struct {
	resolver *net.Resolver
	dialer   *net.Dialer
	mu       sync.Mutex
	pins     map[string][]net.IP
}

func (d *safeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("discovery disallows network %q", network)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	pinKey := net.JoinHostPort(host, port)
	d.mu.Lock()
	ips, ok := d.pins[pinKey]
	if !ok {
		ips, err = d.resolver.LookupIP(ctx, "ip", host)
		if err != nil {
			d.mu.Unlock()
			return nil, err
		}
		safe := make([]net.IP, 0, len(ips))
		for _, ip := range ips {
			if isPublicIP(ip) {
				safe = append(safe, append(net.IP(nil), ip...))
			}
		}
		if len(safe) == 0 {
			d.mu.Unlock()
			return nil, errors.New("hostname resolves only to a disallowed network address")
		}
		ips = safe
		d.pins[pinKey] = ips
	}
	d.mu.Unlock()
	var lastErr error
	for _, ip := range ips {
		conn, dialErr := d.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = errors.New("hostname has no pinned public address")
	}
	return nil, lastErr
}

var blockedPrefixes = mustPrefixes([]string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
	"192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
	"224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "fc00::/7",
	"fe80::/10", "2001:db8::/32", "2001:10::/28",
	"64:ff9b::/96", "64:ff9b:1::/48",
})

func mustPrefixes(raw []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(raw))
	for _, value := range raw {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(err)
		}
		out = append(out, prefix)
	}
	return out
}

func isPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}
