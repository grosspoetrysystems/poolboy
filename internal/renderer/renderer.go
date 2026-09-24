// Package renderer is the constrained Go->Node adapter for the Poolboy
// template companion. One process per template: it writes a JSON request to the
// companion's stdin and reads exactly one JSON response from stdout, under a
// hard time, request and output budget. It never grants network access, reads no
// executable path from corpus metadata, and invokes no project callback.
//
// The wire protocol is renderer protocol v0 (see the repository spec): stdin is
// {"template": "...", "variables": {}} and stdout is a single object, either
// {"ok":true,"output":"...","warnings":[]} on success, a template failure
// {"ok":false,"kind":"template","errors":[...],"warnings":[]}, or a protocol
// failure {"ok":false,"kind":"protocol","code":"...","message":"..."}.
package renderer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Fixed adapter budgets. These are policy, not tuning knobs: the companion runs
// under a bounded heap, a bounded request, bounded output capture, and a hard
// deadline, so a hostile or looping template cannot exhaust the host.
const (
	deadline         = 5 * time.Second // hard per-template wall clock
	maxRequest       = 2 << 20         // 2 MiB request cap (protocol v0)
	maxStdout        = 8 << 20         // 8 MiB captured stdout cap
	maxStderr        = 256 << 10       // 256 KiB captured stderr cap
	nodeHeapMiB      = 128             // --max-old-space-size for the node process
	defaultCompanion = "poolboy-knap.mjs"
)

// Result is a successful render.
type Result struct {
	Output   string
	Warnings []string
}

// TemplateError is one template-level diagnostic reported by the companion.
type TemplateError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

// ErrorKind classifies a render failure.
type ErrorKind string

const (
	// KindTemplate is a template-level failure reported by the companion (exit 1).
	KindTemplate ErrorKind = "template"
	// KindProtocol is a protocol-level failure reported by the companion (exit 2).
	KindProtocol ErrorKind = "protocol"
	// KindAdapter is a failure detected by this adapter: spawn, timeout,
	// oversized or malformed output. The companion is not trusted to report these.
	KindAdapter ErrorKind = "adapter"
)

// RenderError is a structured render failure. It never carries partial output:
// on any failure the emitted document is discarded by the caller.
type RenderError struct {
	Kind    ErrorKind
	Code    string          // protocol/adapter code, e.g. INVALID_REQUEST, TIMEOUT
	Message string          // human-readable detail
	Errors  []TemplateError // template diagnostics (Kind == KindTemplate)
	Stderr  string          // captured stderr snippet, for adapter failures
}

func (e *RenderError) Error() string {
	switch e.Kind {
	case KindTemplate:
		if len(e.Errors) > 0 {
			return fmt.Sprintf("renderer template error: %s: %s (line %d, col %d)",
				e.Errors[0].Code, e.Errors[0].Message, e.Errors[0].Line, e.Errors[0].Column)
		}
		return "renderer template error"
	case KindProtocol:
		return fmt.Sprintf("renderer protocol error: %s: %s", e.Code, e.Message)
	default:
		return fmt.Sprintf("renderer adapter error: %s: %s", e.Code, e.Message)
	}
}

// request is the wire request sent on stdin.
type request struct {
	Template  string         `json:"template"`
	Variables map[string]any `json:"variables"`
}

// response is the wire response read from stdout.
type response struct {
	OK       bool            `json:"ok"`
	Output   string          `json:"output"`
	Warnings []string        `json:"warnings"`
	Kind     string          `json:"kind"`
	Code     string          `json:"code"`
	Message  string          `json:"message"`
	Errors   []TemplateError `json:"errors"`
}

// Resolve returns the renderer command path to run. An explicit path is used as
// given (a trusted installed companion, typically an absolute cli.mjs). An empty
// path resolves to the default companion beside the running executable.
func Resolve(path string) (string, error) {
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("locate executable for default renderer: %w", err)
		}
		path = filepath.Join(filepath.Dir(exe), defaultCompanion)
	}
	// #nosec G304 -- the companion path is trusted configuration or executable-adjacent.
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("companion not found at %s", path)
		}
		return "", fmt.Errorf("stat companion at %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("companion is not a regular file at %s", path)
	}
	return path, nil
}

// Render runs one template through the companion at rendererPath. A ".mjs"/".js"
// renderer is launched under node with a bounded heap; any other path is executed
func Render(ctx context.Context, rendererPath, template string, variables map[string]any) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if variables == nil {
		variables = map[string]any{}
	}
	req, err := json.Marshal(request{Template: template, Variables: variables})
	if err != nil {
		return Result{}, &RenderError{Kind: KindAdapter, Code: "INVALID_REQUEST", Message: err.Error()}
	}
	if len(req) > maxRequest {
		return Result{}, &RenderError{Kind: KindAdapter, Code: "REQUEST_TOO_LARGE",
			Message: fmt.Sprintf("request %d bytes exceeds %d cap", len(req), maxRequest)}
	}

	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	cmd := command(ctx, rendererPath)
	cmd.Env = minimalEnv()
	cmd.Stdin = bytes.NewReader(req)
	stdout := &cappedBuffer{cap: maxStdout}
	stderr := &cappedBuffer{cap: maxStderr}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()

	if ctxErr := ctx.Err(); ctxErr != nil {
		code := "CANCELED"
		message := "renderer canceled"
		if ctxErr == context.DeadlineExceeded {
			code = "TIMEOUT"
			message = fmt.Sprintf("renderer exceeded %s deadline", deadline)
		}
		return Result{}, &RenderError{Kind: KindAdapter, Code: code, Message: message, Stderr: stderr.snippet()}
	}
	if stdout.overflow {
		return Result{}, &RenderError{Kind: KindAdapter, Code: "OVERSIZED_OUTPUT",
			Message: fmt.Sprintf("renderer stdout exceeded %d bytes", maxStdout), Stderr: stderr.snippet()}
	}

	resp, decErr := decode(stdout.buf.Bytes())
	if decErr != nil {
		return Result{}, &RenderError{Kind: KindAdapter, Code: "MALFORMED_OUTPUT",
			Message: adapterMsg(decErr, runErr), Stderr: stderr.snippet()}
	}

	if resp.OK && runErr != nil {
		return Result{}, &RenderError{Kind: KindAdapter, Code: "PROCESS_EXIT",
			Message: fmt.Sprintf("renderer returned success with process error: %v", runErr), Stderr: stderr.snippet()}
	}

	if resp.OK {
		return Result{Output: resp.Output, Warnings: resp.Warnings}, nil
	}
	switch ErrorKind(resp.Kind) {
	case KindTemplate:
		return Result{}, &RenderError{Kind: KindTemplate, Errors: resp.Errors, Message: resp.Message}
	case KindProtocol:
		return Result{}, &RenderError{Kind: KindProtocol, Code: resp.Code, Message: resp.Message}
	default:
		return Result{}, &RenderError{Kind: KindAdapter, Code: "MALFORMED_OUTPUT",
			Message: fmt.Sprintf("response ok=false with unknown kind %q", resp.Kind), Stderr: stderr.snippet()}
	}
}

// command builds the subprocess. A ".mjs"/".js" companion runs under node with a
// bounded heap; anything else is executed directly.
func command(ctx context.Context, rendererPath string) *exec.Cmd {
	lower := strings.ToLower(rendererPath)
	if strings.HasSuffix(lower, ".mjs") || strings.HasSuffix(lower, ".js") {
		// #nosec G204 -- rendererPath is a trusted executable; exec.CommandContext does not invoke a shell.
		return exec.CommandContext(ctx, "node",
			fmt.Sprintf("--max-old-space-size=%d", nodeHeapMiB),
			rendererPath)
	}
	return exec.CommandContext(ctx, rendererPath)
}

// minimalEnv is a controlled environment: PATH only, so node is locatable while
// nothing else (proxies, NODE_OPTIONS, credentials) leaks into the companion.
func minimalEnv() []string {
	env := []string{"PATH=" + os.Getenv("PATH")}
	if sr := os.Getenv("SYSTEMROOT"); sr != "" { // node needs it on Windows
		env = append(env, "SYSTEMROOT="+sr)
	}
	return env
}

// adapterMsg folds a decode failure and any process error into one message.
func adapterMsg(decErr, runErr error) string {
	if runErr != nil {
		return fmt.Sprintf("%v (process: %v)", decErr, runErr)
	}
	return decErr.Error()
}

// cappedBuffer captures up to cap bytes and drains the rest, so an oversized
// producer neither blocks on a full pipe nor exhausts memory. overflow records
// that the cap was hit.
type cappedBuffer struct {
	buf      bytes.Buffer
	cap      int
	overflow bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.cap - c.buf.Len(); room > 0 {
		if len(p) <= room {
			return c.buf.Write(p)
		}
		_, _ = c.buf.Write(p[:room])
	}
	c.overflow = true
	return len(p), nil // report consumed so the process is never blocked on us
}

// decode reads exactly one JSON object from b, rejecting empty input, trailing
// garbage, or a second object.
func decode(b []byte) (response, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	var resp response
	if err := dec.Decode(&resp); err != nil {
		return response{}, fmt.Errorf("decode response: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return response{}, fmt.Errorf("decode response: unexpected second JSON value")
		}
		return response{}, fmt.Errorf("decode response: unexpected trailing data: %w", err)
	}
	return resp, nil
}

func (c *cappedBuffer) snippet() string {
	const snippetMaxBytes = 2 << 10
	s := c.buf.String()
	if len(s) > snippetMaxBytes {
		return s[:snippetMaxBytes] + "…"
	}
	return s
}
