package source

import "bytes"

// secretNameReason classifies exact credential file shapes. It deliberately
// does not classify ordinary source or documentation by words such as "secret"
// or "credential".
func secretNameReason(name string) string {
	lower := lowerASCII(name)
	switch lower {
	case ".env", ".envrc":
		return "environment_credentials"
	case ".netrc", ".npmrc", ".pypirc", ".pgpass", ".git-credentials", ".htpasswd",
		"credentials", "credentials.json", "service-account.json", "serviceaccount.json":
		return "credential_filename"
	case "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return "private_key_filename"
	}
	for _, suffix := range []string{".example", ".sample", ".template", ".defaults"} {
		if lower == ".env"+suffix || (len(lower) > 5 && lower[:5] == ".env." && hasSuffixASCII(lower, suffix)) {
			return ""
		}
	}
	if len(lower) > 5 && lower[:5] == ".env." {
		return "environment_credentials"
	}
	for _, suffix := range []string{".key", ".p12", ".pfx", ".jks", ".keystore", ".ppk"} {
		if hasSuffixASCII(lower, suffix) {
			return "private_key_container"
		}
	}
	return ""
}

// secretContentReason catches complete private-key blocks. Generic words,
// assignments, public identifiers, and public certificate/signature formats are
// ordinary source evidence and are not secret signals by themselves.
func secretContentReason(data []byte) string {
	for _, kind := range []string{"", "RSA ", "EC ", "OPENSSH "} {
		begin := []byte("-----BEGIN " + kind + "PRIVATE KEY-----")
		end := []byte("-----END " + kind + "PRIVATE KEY-----")
		if bytes.Contains(data, begin) && bytes.Contains(data, end) {
			return "private_key_block"
		}
	}
	pgpBegin := []byte("-----BEGIN PGP " + "PRIVATE KEY BLOCK-----")
	pgpEnd := []byte("-----END PGP " + "PRIVATE KEY BLOCK-----")
	if bytes.Contains(data, pgpBegin) && bytes.Contains(data, pgpEnd) {
		return "private_key_block"
	}
	return ""
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

func hasSuffixASCII(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
