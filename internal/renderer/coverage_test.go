package renderer

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderErrorFormatting(t *testing.T) {
	cases := []struct {
		name string
		err  *RenderError
		want string
	}{
		{"template diagnostic", &RenderError{Kind: KindTemplate, Errors: []TemplateError{{Code: "BAD", Message: "nope", Line: 3, Column: 4}}}, "renderer template error: BAD: nope (line 3, col 4)"},
		{"empty template diagnostics", &RenderError{Kind: KindTemplate}, "renderer template error"},
		{"protocol", &RenderError{Kind: KindProtocol, Code: "INVALID", Message: "bad request"}, "renderer protocol error: INVALID: bad request"},
		{"adapter", &RenderError{Kind: KindAdapter, Code: "TIMEOUT", Message: "late"}, "renderer adapter error: TIMEOUT: late"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveErrorExposesStableFieldsAndCause(t *testing.T) {
	sentinel := errors.New("permission denied")
	cases := []struct {
		name string
		err  *ResolveError
		code string
		path string
	}{
		{"not found", &ResolveError{Code: "COMPANION_NOT_FOUND", Path: "/missing"}, "COMPANION_NOT_FOUND", "/missing"},
		{"not regular", &ResolveError{Code: "COMPANION_NOT_REGULAR", Path: "/directory"}, "COMPANION_NOT_REGULAR", "/directory"},
		{"unreadable", &ResolveError{Code: "COMPANION_UNREADABLE", Path: "/blocked", err: sentinel}, "COMPANION_UNREADABLE", "/blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code || tc.err.Path != tc.path {
				t.Fatalf("ResolveError fields = code %q path %q, want %q %q", tc.err.Code, tc.err.Path, tc.code, tc.path)
			}
		})
	}
	if !errors.Is((&ResolveError{Code: "COMPANION_UNREADABLE", err: sentinel}), sentinel) {
		t.Fatal("ResolveError does not unwrap its cause")
	}
}

func TestResolveRejectsDirectoryAndResolvesDefault(t *testing.T) {
	dir := t.TempDir()
	if _, err := Resolve(dir); err == nil {
		t.Fatal("directory companion unexpectedly resolved")
	} else {
		var resolveErr *ResolveError
		if !errors.As(err, &resolveErr) || resolveErr.Code != "COMPANION_NOT_REGULAR" {
			t.Fatalf("directory error = %v", err)
		}
	}
	path, err := Resolve("")
	if err == nil {
		if !strings.HasSuffix(path, defaultCompanion) {
			t.Fatalf("default companion = %q", path)
		}
	} else {
		var resolveErr *ResolveError
		if !errors.As(err, &resolveErr) || !strings.HasSuffix(resolveErr.Path, defaultCompanion) {
			t.Fatalf("default companion error = %v", err)
		}
	}
}

func TestResolveReportsStatFailure(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "bad\x00path")
	_, err := Resolve(badPath)
	var resolveErr *ResolveError
	if err == nil || !errors.As(err, &resolveErr) || resolveErr.Code != "COMPANION_UNREADABLE" {
		t.Fatalf("stat failure = %v", err)
	}
	if resolveErr.Unwrap() == nil {
		t.Fatal("stat failure did not retain cause")
	}
}

func TestMinimalEnvAndAdapterMessages(t *testing.T) {
	t.Setenv("PATH", "/test/path")
	t.Setenv("SYSTEMROOT", "/windows")
	env := minimalEnv()
	if len(env) != 2 || env[0] != "PATH=/test/path" || env[1] != "SYSTEMROOT=/windows" {
		t.Fatalf("minimal environment = %#v", env)
	}
	decodeErr := errors.New("decode failed")
	if got := adapterMsg(decodeErr, nil); got != "decode failed" {
		t.Fatalf("adapterMsg without process error = %q", got)
	}
	if got := adapterMsg(decodeErr, errors.New("exit status 2")); !strings.Contains(got, "decode failed") || !strings.Contains(got, "process: exit status 2") {
		t.Fatalf("adapterMsg with process error = %q", got)
	}
}

func TestCappedBufferAndDecodeBoundaries(t *testing.T) {
	var c cappedBuffer
	c.cap = 4
	if n, err := c.Write([]byte("abcdef")); err != nil || n != 6 || c.buf.String() != "abcd" || !c.overflow {
		t.Fatalf("capped write n=%d err=%v value=%q overflow=%v", n, err, c.buf.String(), c.overflow)
	}
	largeInput := bytes.Repeat([]byte("x"), 4096)
	large := &cappedBuffer{cap: len(largeInput)}
	if _, err := large.Write(largeInput); err != nil {
		t.Fatal(err)
	}
	snippet := large.snippet()
	if len(snippet) >= len(large.buf.String()) {
		t.Fatalf("large output was not bounded: snippet=%d full=%d", len(snippet), large.buf.Len())
	}

	if _, err := decode(nil); err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("empty response error = %v", err)
	}
	if _, err := decode([]byte(`{"ok":true} trailing`)); err == nil || !strings.Contains(err.Error(), "unexpected trailing data") {
		t.Fatalf("trailing response error = %v", err)
	}
}

func TestRenderReportsProcessFailureAndCancellation(t *testing.T) {
	successThenFail := script(t, "#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"output\":\"ok\"}'\nexit 1\n")
	_, err := Render(context.Background(), successThenFail, "x", nil)
	var renderErr *RenderError
	if !errors.As(err, &renderErr) || renderErr.Code != "PROCESS_EXIT" {
		t.Fatalf("success-with-process-error = %#v", err)
	}

	cancel, cancelFn := context.WithCancel(context.Background())
	cancelFn()
	loop := script(t, "#!/bin/sh\nwhile :; do :; done\n")
	_, err = Render(cancel, loop, "x", nil)
	if !errors.As(err, &renderErr) || renderErr.Code != "CANCELED" {
		t.Fatalf("canceled render = %#v", err)
	}
}

func TestRenderRejectsUnknownResponseKind(t *testing.T) {
	unknown := script(t, "#!/bin/sh\nprintf '%s\\n' '{\"ok\":false,\"kind\":\"other\"}'\n")
	_, err := Render(context.Background(), unknown, "x", nil)
	var renderErr *RenderError
	if !errors.As(err, &renderErr) || renderErr.Code != "MALFORMED_OUTPUT" || !strings.Contains(renderErr.Message, "unknown kind") {
		t.Fatalf("unknown response = %#v", err)
	}
}
