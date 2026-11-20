package workbench

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Credentials do not belong in an agent's files: they are passed by reference. A value is a
// reference only when it is entirely ${VARIABLE}, Bearer ${VARIABLE}, or a keychain://,
// file:// or env:// reference. The checks below look at the names (a field called *_TOKEN
// with a literal value) and at the values themselves (the shapes of well-known keys).

var (
	// highConfidence are the shapes of real credentials: their presence anywhere refuses an
	// export.
	highConfidence = []struct {
		name string
		re   *regexp.Regexp
	}{
		{"an Anthropic key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{10,}`)},
		{"an API key (sk-)", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
		{"a GitHub token", regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`)},
		{"a Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
		{"an AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
		{"a private key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
		{"a password in a URL", regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@'"]+:[^/\s@'"$]+@`)},
		{"a credential in a URL query", regexp.MustCompile(`(?i)[?&](api[_-]?key|access[_-]?token|token|secret|password|key)=[^&\s'"$]{6,}`)},
		{"a bearer token", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{20,}`)},
	}
	// userinfoRe is a URL with a user part (a token often goes there): a warning.
	userinfoRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@'"$]{6,}@`)
	// lowConfidence looks like a credential assignment in prose or a resource file: a warning.
	lowConfidence = regexp.MustCompile(`(?i)\b(api[_-]?key|secret|password|passwd|token|bearer)\b["']?\s*[:=]\s*["']?([A-Za-z0-9_\-./+]{16,})`)

	varRe    = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)
	bearerRe = regexp.MustCompile(`^(?i:bearer)\s+\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)
)

// isRef reports whether v passes a credential by reference.
func isRef(v string) bool {
	v = strings.TrimSpace(v)
	if varRe.MatchString(v) || bearerRe.MatchString(v) {
		return true
	}
	for _, scheme := range []string{"keychain://", "file://", "env://"} {
		if strings.HasPrefix(v, scheme) && len(v) > len(scheme) {
			return true
		}
	}
	return false
}

// pattern names the well-known credential shape in s, or "".
func pattern(s string) string {
	for _, p := range highConfidence {
		if p.re.MatchString(s) {
			return p.name
		}
	}
	return ""
}

// secretName: a field named like a credential (API_KEY, X-Auth-Token, GITHUB_PAT).
func secretName(k string) bool {
	k = strings.ToLower(k)
	for _, w := range []string{"key", "token", "secret", "auth", "password", "passwd", "credential"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return k == "pat" || strings.HasSuffix(k, "_pat") || strings.HasSuffix(k, "-pat")
}

// secretValue says why the value of field name is a credential written in the file, or "".
// A known credential shape counts even inside a reference (file://sk-...).
func secretValue(name, v string) string {
	if strings.TrimSpace(v) == "" {
		return ""
	}
	if p := pattern(v); p != "" {
		return "holds " + p
	}
	if isRef(v) {
		return ""
	}
	if secretName(name) {
		return "holds a literal value where a credential is named"
	}
	return ""
}

// urlSecret says why a URL carries a credential, or "".
func urlSecret(raw string) string {
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if pw, set := u.User.Password(); set && pw != "" && !isRef(pw) {
			return "holds a password (user:password@)"
		}
		if u.User != nil && u.User.Username() != "" && !isRef(u.User.Username()) {
			return "holds a user part (user@), where tokens are often put; pass it by reference"
		}
		for k, vs := range u.Query() {
			for _, v := range vs {
				if v != "" && !isRef(v) && (secretName(k) || pattern(v) != "") {
					return fmt.Sprintf("holds a credential in its query (%s=)", k)
				}
			}
		}
	}
	if p := pattern(raw); p != "" {
		return "holds " + p
	}
	return ""
}

// ScanFinding is a credential-like text in one of the agent's files.
type ScanFinding struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	What   string `json:"what"`
	Refuse bool   `json:"refuse"` // a high-confidence shape: an export is refused
}

// scanTree looks through every file (prompts, skills and their resources, context, sidecar)
// for credentials: well-known shapes refuse an export, credential-like assignments warn.
func scanTree(t Tree) []ScanFinding {
	var out []ScanFinding
	for _, p := range t.Paths() {
		for i, line := range strings.Split(string(t[p]), "\n") {
			if what := pattern(line); what != "" {
				out = append(out, ScanFinding{File: p, Line: i + 1, What: what, Refuse: true})
				continue
			}
			if m := lowConfidence.FindStringSubmatch(line); m != nil && !isRef(m[2]) && !strings.Contains(m[2], "${") {
				out = append(out, ScanFinding{File: p, Line: i + 1, What: "what looks like a " + strings.ToLower(m[1]) + " assignment"})
				continue
			}
			if userinfoRe.MatchString(line) {
				out = append(out, ScanFinding{File: p, Line: i + 1, What: "a URL with a user part, where tokens are often put"})
			}
		}
	}
	return out
}
