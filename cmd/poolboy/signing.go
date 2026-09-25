package main

import (
	"bufio"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grosspoetrysystems/poolboy/bundle"
	"github.com/grosspoetrysystems/poolboy/internal/sign"
	"golang.org/x/term"
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
	if err := refuseKeyInPublication(*out); err != nil {
		return productError(err)
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
	fmt.Fprintf(os.Stderr, "poolboy: wrote the private signing key to %s — keep it out of version control (add it to .gitignore)\n", *out)
	return 0
}

// refuseKeyInPublication rejects a private key path inside the corpus or the
// publication output. A key committed beside the docs, or copied into dist/ by
// a build, is a disclosed key.
func refuseKeyInPublication(out string) error {
	abs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	b, err := bundle.Discover(".")
	if err != nil {
		// Not inside a project: there is no corpus or output to protect.
		return nil
	}
	for label, dir := range map[string]string{
		"corpus":      b.Dir,
		"publication": filepath.Join(b.Root, filepath.FromSlash(b.Output)),
	} {
		if pathWithin(abs, dir) {
			return fmt.Errorf("refusing to write a private signing key inside the %s directory %s; write it outside the project with --out", label, dir)
		}
	}
	return nil
}

func pathWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

type trustFlags struct {
	lock     *string
	tofu     *bool
	identity *string
	channel  *string
}

func addTrustFlags(fs *flag.FlagSet) trustFlags {
	return trustFlags{
		lock:     fs.String("lock", "", "authoritative project trust lock"),
		tofu:     fs.Bool("tofu", false, "explicitly use weaker trust-on-first-use"),
		identity: fs.String("identity", "", "exact expected Sigstore certificate identity"),
		channel:  fs.String("channel", "", "release channel (default stable)"),
	}
}

func (f trustFlags) options() sign.VerifyOptions {
	return sign.VerifyOptions{LockPath: *f.lock, TOFU: *f.tofu, Identity: *f.identity, Channel: *f.channel}
}

func cmdVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	trust := addTrustFlags(fs)
	target, ok := parseWithArg(fs, args)
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: poolboy verify <dir-or-url> [--lock file | --identity URI | --tofu] [--channel name]")
		return 2
	}
	result, err := sign.Verify(target, trust.options())
	if err != nil {
		return verificationError(err)
	}
	if err := printCandidate(result); err != nil {
		return productError(err)
	}
	if result.Pending {
		fmt.Fprintf(os.Stderr, "poolboy: UPDATE PENDING — approve exactly sha256:%s before consuming this corpus\n", result.Record.ManifestSHA256)
		return 1
	}
	if _, err := fmt.Fprintf(os.Stdout, "verified sha256:%s (%d publication files)\n", result.Record.ManifestSHA256, result.Checked); err != nil {
		return productError(err)
	}
	return 0
}

func cmdApprove(args []string) int {
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	trust := addTrustFlags(fs)
	minimumAge := fs.String("minimum-release-age", "", "minimum trusted release age (default 72h for stable Sigstore releases)")
	overrideAge := fs.Bool("override-age", false, "human emergency override for this exact digest")
	migrate := fs.Bool("migrate-identity", false, "replace the approved publisher identity in one approval")
	target, ok := parseWithArg(fs, args)
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: poolboy approve <dir-or-url> [--lock file] [--identity URI | --tofu] [--channel name] [--minimum-release-age 72h]")
		return 2
	}
	opts := trust.options()
	opts.OverrideAge = *overrideAge
	opts.MigrateIdentity = *migrate
	if *minimumAge != "" {
		age, err := time.ParseDuration(*minimumAge)
		if err != nil || age < 0 {
			return productError(fmt.Errorf("invalid --minimum-release-age %q", *minimumAge))
		}
		opts.MinimumReleaseAge = age
	}
	candidate, err := sign.PrepareApproval(target, opts)
	if err != nil {
		return verificationError(err)
	}
	if err := printCandidate(candidate); err != nil {
		return productError(err)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return productError(errors.New("approval requires an interactive terminal"))
	}
	fmt.Fprintf(os.Stderr, "Approve exactly sha256:%s? [y/N] ", candidate.Record.ManifestSHA256)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return productError(err)
	}
	if strings.TrimSpace(strings.ToLower(answer)) != "y" {
		fmt.Fprintln(os.Stderr, "poolboy: approval cancelled")
		return 1
	}
	if err := sign.CommitApproval(candidate, opts); err != nil {
		return productError(err)
	}
	destination := "local trust store"
	if opts.LockPath != "" {
		destination = opts.LockPath
	}
	if _, err := fmt.Fprintf(os.Stdout, "approved sha256:%s in %s\n", candidate.Record.ManifestSHA256, destination); err != nil {
		return productError(err)
	}
	return 0
}

func verificationError(err error) int {
	var negative *sign.VerifyError
	if errors.As(err, &negative) {
		fmt.Fprintln(os.Stderr, "poolboy:", err)
		return 1
	}
	return productError(err)
}

func printCandidate(candidate *sign.Candidate) error {
	record := candidate.Record
	lines := []string{
		"publisher mode: " + record.Mode,
		"origin: " + record.Origin,
		"channel: " + record.Channel,
		"manifest: sha256:" + record.ManifestSHA256,
	}
	if candidate.Previous != nil {
		previous := candidate.Previous
		lines = append(lines, "approved manifest: sha256:"+previous.ManifestSHA256)
		if !previous.SignedAt.IsZero() {
			lines = append(lines, "approved signing time: "+previous.SignedAt.Format(time.RFC3339))
		}
	}
	if record.Identity != "" {
		lines = append(lines, "identity: "+record.Identity, "signed: "+record.SignedAt.Format(time.RFC3339))
	} else {
		lines = append(lines, "publisher key: "+record.Key+" (TOFU continuity only; identity unconfirmed)")
	}
	for _, change := range []struct {
		label string
		paths []string
	}{
		{"added", candidate.Added},
		{"changed", candidate.Changed},
		{"removed", candidate.Removed},
	} {
		if len(change.paths) > 0 {
			lines = append(lines, fmt.Sprintf("%s (%d): %s", change.label, len(change.paths), strings.Join(change.paths, ", ")))
		}
	}
	_, err := fmt.Fprintln(os.Stdout, strings.Join(lines, "\n"))
	return err
}
