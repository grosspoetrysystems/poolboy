package main

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/internal/sign"
)

func cmdKeygen(args []string) int {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "poolboy.key", "private signing key file")
	force := fs.Bool("force", false, "overwrite an existing key file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		return productError(errors.New("keygen accepts no positional arguments"))
	}
	seed, pub, err := sign.GenerateSeed()
	if err != nil {
		return productError(err)
	}
	if err := sign.WriteSeedFile(*out, seed, *force); err != nil {
		return productError(err)
	}
	if _, err := fmt.Fprintln(os.Stdout, pub); err != nil {
		return productError(err)
	}
	return 0
}

func cmdSign(args []string) int {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	keyPath := fs.String("key", "", "private signing key file (or POOLBOY_SIGNING_KEY)")
	localRoot := fs.String("root", "", "project root or directory containing poolboy.toml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		return productError(errors.New("sign accepts no positional arguments"))
	}
	start := rootDir
	if *localRoot != "" {
		start = *localRoot
	}
	if start == "" {
		start = "."
	}
	b, err := bundle.Discover(start)
	if err != nil {
		return productError(err)
	}
	var priv ed25519.PrivateKey
	if *keyPath != "" {
		priv, err = sign.LoadSeedFile(*keyPath)
	} else {
		value := os.Getenv("POOLBOY_SIGNING_KEY")
		if value == "" {
			return productError(errors.New("signing key required: pass --key or set POOLBOY_SIGNING_KEY"))
		}
		priv, err = sign.ParseSeed(value)
		if err != nil {
			// Accept a path as a compatibility convenience, while the
			// environment variable's primary form is the seed itself.
			priv, err = sign.LoadSeedFile(value)
		}
	}
	if err != nil {
		return productError(fmt.Errorf("read signing key: %w", err))
	}
	output := filepath.Join(b.Root, filepath.FromSlash(b.Output))
	if err := sign.Dir(output, priv); err != nil {
		return productError(err)
	}
	if _, err := fmt.Fprintln(os.Stdout, "signed publication:", output); err != nil {
		return productError(err)
	}
	return 0
}

func cmdVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	full := fs.Bool("full", false, "verify every Markdown file and artifact hash")
	target, ok := parseWithArg(fs, args)
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: poolboy verify <dir-or-url> [--full]")
		return 2
	}
	result, err := sign.Verify(target, *full)
	if err != nil {
		var negative *sign.VerifyError
		if errors.As(err, &negative) {
			fmt.Fprintln(os.Stderr, "poolboy:", err)
			return 1
		}
		return productError(err)
	}
	if result.Pinned {
		if _, err := fmt.Fprintf(os.Stdout, "verified publisher %s (pinned new publisher for %s)\n", result.Key, result.Origin); err != nil {
			return productError(err)
		}
	} else if _, err := fmt.Fprintf(os.Stdout, "verified publisher %s for %s\n", result.Key, result.Origin); err != nil {
		return productError(err)
	}
	if result.Full {
		if _, err := fmt.Fprintf(os.Stdout, "verified %d publication files\n", result.Checked); err != nil {
			return productError(err)
		}
	}
	return 0
}
