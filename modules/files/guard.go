package files

import (
	"path"
	"path/filepath"
	"strings"
)

// The denylist: what Files never reads, writes, moves or deletes, whoever asks. It is a
// guard against handing a credential to a model or a page by accident, not an access
// control system. Everything is matched in lower case, on slash-separated paths.
var (
	// secretDirs are directories whose whole contents are refused.
	secretDirs = map[string]bool{".ssh": true, ".gnupg": true}
	// secretTails are refused when a path is one or ends with one.
	secretTails = []string{".aws/credentials", ".kube/config", ".docker/config.json"}
	// secretNames are file name patterns (path.Match) refused anywhere.
	secretNames = []string{
		"*.pem", "*.key", "*.kdbx", "*.p12", "*.pfx",
		"id_rsa*", "id_dsa*", "id_ecdsa*", "id_ed25519*",
		".env", ".env.*", ".netrc", ".pgpass", ".npmrc", ".pypirc",
	}
	// envExamples are the .env.* names that hold no values and stay readable.
	envExamples = map[string]bool{".env.example": true, ".env.sample": true, ".env.template": true}
)

// sensitive reports whether a path looks like it holds a credential. p may be relative to
// a place or absolute; either separator is accepted.
//
// A path is refused when it lies in a secretDirs directory, ends with one of secretTails,
// has a name matching secretNames, or has "token" or "secret" in its name while lying
// under a dot-directory (~/.config/gh/token.json). A file named "secrets.md" in an
// ordinary folder is the owner's own writing and is left alone.
func sensitive(p string) bool {
	p = strings.ToLower(filepath.ToSlash(p))
	segs := strings.Split(strings.Trim(path.Clean(p), "/"), "/")
	name := segs[len(segs)-1]
	underDot := false
	for i, s := range segs {
		if secretDirs[s] {
			return true
		}
		if i < len(segs)-1 && strings.HasPrefix(s, ".") && s != "." && s != ".." {
			underDot = true
		}
	}
	for _, tail := range secretTails {
		if p == tail || strings.HasSuffix(p, "/"+tail) {
			return true
		}
	}
	if envExamples[name] {
		return false
	}
	for _, pat := range secretNames {
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
	}
	return underDot && (strings.Contains(name, "token") || strings.Contains(name, "secret"))
}
