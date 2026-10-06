// Package pathmatch implements the include/exclude/.gitignore path matching
// shared by the watcher and debouncer packages. It used to be duplicated in
// both, which let their behavior drift (the watcher's directory-exclude check
// once matched against absolute paths instead of paths relative to the
// project root, while the debouncer's never had that bug) - a single
// implementation makes that class of drift impossible going forward.
package pathmatch

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Matcher answers include/exclude questions about paths relative to a base
// directory (typically the project's working directory).
type Matcher struct {
	baseDir   string
	includes  []string
	excludes  []string
	gitignore []string
}

// New builds a Matcher. includes/excludes are glob-ish patterns (see
// patternMatch); gitignore is a pre-parsed list of .gitignore entries applied
// as additional excludes.
func New(baseDir string, includes, excludes, gitignore []string) *Matcher {
	return &Matcher{baseDir: baseDir, includes: includes, excludes: excludes, gitignore: gitignore}
}

// LoadGitignore reads <baseDir>/.gitignore and returns its non-comment,
// non-negated entries. A missing file is not an error.
func LoadGitignore(baseDir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(baseDir, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		trim := strings.TrimSpace(scanner.Text())
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "!") {
			continue
		}
		out = append(out, trim)
	}
	return out, nil
}

// Included reports whether path matches at least one include pattern.
func (m *Matcher) Included(path string) bool {
	rel, base := m.relBase(path)
	for _, p := range m.includes {
		if patternMatch(rel, base, p) {
			return true
		}
	}
	return false
}

// ExcludedFile reports whether path matches any file-level exclude or
// gitignore pattern.
func (m *Matcher) ExcludedFile(path string) bool {
	rel, base := m.relBase(path)
	for _, p := range m.excludes {
		if patternMatch(rel, base, p) {
			return true
		}
	}
	for _, p := range m.gitignore {
		if patternMatch(rel, base, p) {
			return true
		}
	}
	return false
}

// MatchFile reports whether path should be treated as a watched/fingerprinted
// file: included and not excluded.
func (m *Matcher) MatchFile(path string) bool {
	return !m.ExcludedFile(path) && m.Included(path)
}

// ExcludedDir reports whether a directory (and everything under it) should be
// pruned entirely during a walk. Only trailing-slash ("dir/") patterns are
// considered here - a file-style glob like "*.pb.go" must never cause an
// entire subtree to be skipped just because it happens to match a directory's
// own name.
func (m *Matcher) ExcludedDir(path string) bool {
	rel, err := filepath.Rel(m.baseDir, path)
	if err != nil {
		rel = path
	}
	clean := filepath.ToSlash(rel) + "/"
	for _, p := range m.excludes {
		if strings.HasSuffix(p, "/") && strings.Contains(clean, trimLeadingDotSlash(filepath.ToSlash(p))) {
			return true
		}
	}
	for _, p := range m.gitignore {
		if strings.HasSuffix(p, "/") && strings.Contains(clean, trimLeadingDotSlash(filepath.ToSlash(p))) {
			return true
		}
	}
	return false
}

func (m *Matcher) relBase(path string) (rel, base string) {
	r, _ := filepath.Rel(m.baseDir, path)
	rel = filepath.ToSlash(r)
	base = filepath.Base(path)
	if rel == "" || rel == "." {
		rel = filepath.ToSlash(path)
	}
	return rel, base
}

// patternMatch matches a pattern against either the file's basename or its
// path relative to the base directory. A trailing "/" makes it a directory
// prefix match; otherwise it's a glob match on basename or relative path,
// falling back to a raw substring match on the relative path.
//
// That substring fallback is intentionally loose (matches "test" against
// "internal/latest/x.go") - it's a known, documented footgun for
// hand-written patterns, kept as-is here rather than changed silently since
// it could alter behavior for existing xgo.yaml files.
func patternMatch(relPath, base, pattern string) bool {
	pat := trimLeadingDotSlash(filepath.ToSlash(strings.TrimSpace(pattern)))
	if pat == "" {
		return false
	}
	if strings.HasSuffix(pat, "/") {
		rel := trimLeadingDotSlash(relPath)
		return strings.HasPrefix(rel, pat) || strings.Contains("/"+rel+"/", "/"+pat)
	}
	if ok, _ := filepath.Match(pat, base); ok {
		return true
	}
	if ok, _ := filepath.Match(pat, relPath); ok {
		return true
	}
	return strings.Contains(relPath, pat)
}

func trimLeadingDotSlash(s string) string {
	return strings.TrimPrefix(strings.TrimPrefix(s, "./"), "/")
}
