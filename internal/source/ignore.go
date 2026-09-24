package source

import "bytes"

// likelySecretName is deliberately conservative: excluding a plausible
// credential file is safer than hashing it into an inventory that invites
// discovery. Detection returns only a boolean; callers never log content.
func likelySecretName(name string) bool {
	lower := lowerASCII(name)
	switch lower {
	case ".env", ".netrc", ".npmrc", ".pypirc", ".pgpass", ".git-credentials", ".htpasswd",
		"credentials", "credentials.json", "service-account.json", "serviceaccount.json",
		"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	for _, safe := range []string{".env.example", ".env.sample", ".env.template", ".env.defaults"} {
		if lower == safe {
			return false
		}
	}
	if len(lower) > 4 && lower[:4] == ".env" {
		return true
	}
	for _, marker := range []string{"secret", "credential", "password", "passwd", "private-key", "private_key"} {
		if containsASCII(lower, marker) {
			return true
		}
	}
	for _, suffix := range []string{".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".ppk", ".asc", ".gpg"} {
		if hasSuffixASCII(lower, suffix) {
			return true
		}
	}
	return false
}

// likelySecretContent catches common private-key and credential markers. The
// checks are intentionally bounded and coarse; the inventory is an inspection
// boundary, not a guarantee that every secret is absent.
func likelySecretContent(data []byte) bool {
	for _, marker := range [][]byte{
		[]byte("-----BEGIN PRIVATE KEY-----"),
		[]byte("-----BEGIN RSA PRIVATE KEY-----"),
		[]byte("-----BEGIN EC PRIVATE KEY-----"),
		[]byte("-----BEGIN OPENSSH PRIVATE KEY-----"),
		[]byte("aws_secret_access_key"),
		[]byte("AWS_SECRET_ACCESS_KEY"),
		[]byte("secret="),
		[]byte("secret:"),
		[]byte("password="),
		[]byte("password:"),
		[]byte("passwd="),
		[]byte("token="),
		[]byte("access_token="),
		[]byte("api_key="),
		[]byte("api-key="),
		[]byte("client_secret="),
		[]byte("client-secret="),
		[]byte("github_pat_"),
		[]byte("ghp_"),
		[]byte("gho_"),
		[]byte("ghs_"),
		[]byte("ghr_"),
		[]byte("xoxb-"),
		[]byte("xoxp-"),
		[]byte("sk-live-"),
		[]byte("sk_test_"),
		[]byte("-----BEGIN PGP PRIVATE KEY BLOCK-----"),
	} {
		if bytes.Contains(data, marker) {
			return true
		}
	}
	return hasAWSAccessKey(data) || hasGooglePrivateKey(data)
}

func hasAWSAccessKey(data []byte) bool {
	for i := range len(data) {
		if i+20 > len(data) {
			break
		}
		if (bytes.Equal(data[i:i+4], []byte("AKIA")) || bytes.Equal(data[i:i+4], []byte("ASIA"))) &&
			allAWSChars(data[i+4:i+20]) {
			return true
		}
	}
	return false
}

func allAWSChars(data []byte) bool {
	for _, c := range data {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func hasGooglePrivateKey(data []byte) bool {
	return bytes.Contains(data, []byte(`"private_key"`)) &&
		(bytes.Contains(data, []byte("BEGIN PRIVATE KEY")) || bytes.Contains(data, []byte("\\nMII")))
}

func lowerASCII(s string) string {
	buf := []byte(s)
	for i, c := range buf {
		if c >= 'A' && c <= 'Z' {
			buf[i] = c + ('a' - 'A')
		}
	}
	return string(buf)
}

func containsASCII(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}

func hasSuffixASCII(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
