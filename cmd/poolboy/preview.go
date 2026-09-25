package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grosspoetrysystems/poolboy/internal/compiler"
)

func cmdPreview(args []string) int {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	port := fs.Int("port", 0, "loopback port (default: choose an available port)")
	renderer := fs.String("renderer", "", "trusted Knap companion path (default: poolboy-knap.mjs beside poolboy)")
	if code := productFlags(fs, args); code >= 0 {
		return code
	}
	if fs.NArg() != 0 {
		return productError(errors.New("preview accepts no positional arguments"))
	}
	if *port < 0 || *port > 65535 {
		return productError(errors.New("preview port must be between 0 and 65535"))
	}
	b, err := productBundle()
	if err != nil {
		return productError(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := compiler.Build(ctx, b, *renderer); err != nil {
		return productError(err)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)))
	if err != nil {
		return productError(fmt.Errorf("start preview: %w", err))
	}
	defer func() { _ = listener.Close() }()
	token, err := previewToken()
	if err != nil {
		return productError(err)
	}
	output := filepath.Join(b.Root, filepath.FromSlash(b.Output))
	server := &http.Server{
		Handler:           previewHandler(output, token),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/%s/", listener.Addr().(*net.TCPAddr).Port, token)
	if _, err := fmt.Fprintf(os.Stdout, "Poolboy preview\n\nSource:  %s\nCorpus:  %s\nServing: %s\n\nNothing has been published. Press Ctrl-C to stop.\n", b.Root, output, url); err != nil {
		return productError(err)
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return productError(fmt.Errorf("serve preview: %w", err))
	}
	return 0
}

func previewToken() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("create preview URL: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

func previewHandler(root, token string) http.Handler {
	prefix := "/" + token + "/"
	files := http.StripPrefix(prefix, http.FileServer(http.Dir(root)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if parsed, _, err := net.SplitHostPort(r.Host); err == nil {
			host = parsed
		}
		host = strings.Trim(host, "[]")
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, "loopback host required", http.StatusMisdirectedRequest)
			return
		}
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
}
