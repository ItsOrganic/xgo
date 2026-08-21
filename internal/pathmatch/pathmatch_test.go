package pathmatch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExcludedDir_RelativeNotAbsolute(t *testing.T) {
	// Regression test for the bug fixed this session: isDirExcluded used to
	// match the exclude pattern against the *absolute* path, so a project
	// living anywhere under a directory literally containing "tmp" (like the
	// OS temp dir itself) had its entire root excluded from watching, since
	// "/tmp/whack-test/" contains the substring "tmp/" regardless of the
	// project's own structure. The fix relativizes against baseDir first.
	base := "/tmp/whack-test-project"
	m := New(base, []string{"*.go"}, []string{"tmp/", ".git/"}, nil)

	if m.ExcludedDir(base) {
		t.Fatalf("project root must not be excluded just because the OS temp dir happens to contain 'tmp' in its path")
	}
	if !m.ExcludedDir(filepath.Join(base, "tmp")) {
		t.Fatalf("a genuine tmp/ subdirectory of the project must still be excluded")
	}
	if !m.ExcludedDir(filepath.Join(base, ".git")) {
		t.Fatalf(".git/ subdirectory must be excluded")
	}
	if m.ExcludedDir(filepath.Join(base, "internal")) {
		t.Fatalf("an unrelated subdirectory must not be excluded")
	}
}

func TestExcludedDir_OnlyTrailingSlashPatterns(t *testing.T) {
	// A file-style glob (no trailing slash) must never cause an entire
	// subtree to be pruned during a walk, even if it happens to match a
	// directory's own basename.
	base := "/project"
	m := New(base, []string{"*.go"}, []string{"*.pb.go"}, nil)
	if m.ExcludedDir(filepath.Join(base, "pb.go")) {
		t.Fatalf("a file-style pattern must not exclude a directory")
	}
}

func TestIncludedAndExcludedFile(t *testing.T) {
	base := "/project"
	m := New(base, []string{"*.go"}, []string{"*_test.go", "vendor/"}, nil)

	cases := []struct {
		path            string
		wantIncluded    bool
		wantExcludeFile bool
		wantMatch       bool
	}{
		{filepath.Join(base, "main.go"), true, false, true},
		{filepath.Join(base, "main_test.go"), true, true, false},
		{filepath.Join(base, "README.md"), false, false, false},
		{filepath.Join(base, "vendor", "pkg", "x.go"), true, true, false},
	}
	for _, c := range cases {
		if got := m.Included(c.path); got != c.wantIncluded {
			t.Errorf("Included(%s) = %v, want %v", c.path, got, c.wantIncluded)
		}
		if got := m.ExcludedFile(c.path); got != c.wantExcludeFile {
			t.Errorf("ExcludedFile(%s) = %v, want %v", c.path, got, c.wantExcludeFile)
		}
		if got := m.MatchFile(c.path); got != c.wantMatch {
			t.Errorf("MatchFile(%s) = %v, want %v", c.path, got, c.wantMatch)
		}
	}
}

func TestGitignoreRespected(t *testing.T) {
	base := "/project"
	m := New(base, []string{"*.go"}, nil, []string{"dist/"})
	if !m.ExcludedDir(filepath.Join(base, "dist")) {
		t.Fatalf("a gitignore'd directory must be excluded")
	}
	if !m.ExcludedFile(filepath.Join(base, "dist", "x.go")) {
		t.Fatalf("a file under a gitignore'd directory must be excluded")
	}
}

func TestLoadGitignore(t *testing.T) {
	dir := t.TempDir()
	content := "# comment\n\n!negated/\ntmp/\nbuild/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadGitignore(dir)
	if err != nil {
		t.Fatalf("LoadGitignore: %v", err)
	}
	want := []string{"tmp/", "build/"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadGitignore_Missing(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadGitignore(dir)
	if err != nil {
		t.Fatalf("a missing .gitignore must not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no entries, got %v", got)
	}
}
