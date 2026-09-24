package renderer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderSuccessPassesRequestAndResponse(t *testing.T) {
	r := script(t, `#!/bin/sh
read request
case "$request" in
  *'"template":"hello"'*'"name":"Ada"'*) printf '%s\n' '{"ok":true,"output":"# Hello Ada\n","warnings":["example"]}' ;;
  *) printf '%s\n' '{"ok":false,"kind":"protocol","code":"INVALID_REQUEST","message":"bad request"}'; exit 2 ;;
esac
`)
	got, err := Render(context.Background(), r, "hello", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output != "# Hello Ada\n" || len(got.Warnings) != 1 || got.Warnings[0] != "example" {
		t.Fatalf("result = %#v", got)
	}
}

func TestRenderTemplateFailureHasNoOutput(t *testing.T) {
	r := script(t, `#!/bin/sh
printf '%s\n' '{"ok":false,"kind":"template","errors":[{"code":"UNKNOWN_FILTER","message":"nope","line":2,"column":3}],"warnings":[]}'
exit 1
`)
	got, err := Render(context.Background(), r, "bad", nil)
	if got.Output != "" {
		t.Fatalf("failure returned output %q", got.Output)
	}
	var re *RenderError
	if !errors.As(err, &re) || re.Kind != KindTemplate || len(re.Errors) != 1 || re.Errors[0].Line != 2 {
		t.Fatalf("error = %#v", err)
	}
}

func TestRenderProtocolAndMalformedResponses(t *testing.T) {
	protocol := script(t, `#!/bin/sh
printf '%s\n' '{"ok":false,"kind":"protocol","code":"INVALID_REQUEST","message":"bad"}'
exit 2
`)
	_, err := Render(context.Background(), protocol, "x", nil)
	var re *RenderError
	if !errors.As(err, &re) || re.Kind != KindProtocol || re.Code != "INVALID_REQUEST" {
		t.Fatalf("protocol error = %#v", err)
	}

	malformed := script(t, "#!/bin/sh\nprintf '%s\\n' 'not json'\n")
	_, err = Render(context.Background(), malformed, "x", nil)
	if !errors.As(err, &re) || re.Kind != KindAdapter || re.Code != "MALFORMED_OUTPUT" {
		t.Fatalf("malformed error = %#v", err)
	}

	multiple := script(t, "#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"output\":\"x\"} {\"ok\":true}'\n")
	_, err = Render(context.Background(), multiple, "x", nil)
	if !errors.As(err, &re) || re.Code != "MALFORMED_OUTPUT" {
		t.Fatalf("multiple response error = %#v", err)
	}
}

func TestRenderTimeoutRespectsCallerDeadline(t *testing.T) {
	r := script(t, "#!/bin/sh\nwhile :; do :; done\n")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := Render(ctx, r, "x", nil)
	var re *RenderError
	if !errors.As(err, &re) || re.Code != "TIMEOUT" {
		t.Fatalf("timeout error = %#v", err)
	}
}

func TestRenderRejectsOversizedRequestAndOutput(t *testing.T) {
	_, err := Render(context.Background(), script(t, "#!/bin/sh\nexit 0\n"), strings.Repeat("x", maxRequest), nil)
	var re *RenderError
	if !errors.As(err, &re) || re.Code != "REQUEST_TOO_LARGE" {
		t.Fatalf("request error = %#v", err)
	}

	r := script(t, "#!/bin/sh\nprintf '{\"ok\":true,\"output\":\"'; dd if=/dev/zero bs=1024 count=9000 2>/dev/null | tr '\\000' x; printf '\"}\\n'\n")
	_, err = Render(context.Background(), r, "x", nil)
	if !errors.As(err, &re) || re.Code != "OVERSIZED_OUTPUT" {
		t.Fatalf("output error = %#v", err)
	}
}

func TestResolveConfiguredCompanion(t *testing.T) {
	companion := script(t, "#!/bin/sh\n")
	got, err := Resolve(companion)
	if err != nil {
		t.Fatal(err)
	}
	if got != companion {
		t.Fatalf("configured renderer = %q, want %q", got, companion)
	}
}

func TestResolveMissingCompanion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.mjs")
	_, err := Resolve(path)
	if err == nil || err.Error() != "companion not found at "+path {
		t.Fatalf("missing companion error = %v", err)
	}
}

func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "renderer-stub.sh")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
