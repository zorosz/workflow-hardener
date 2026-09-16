package hardener

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testInputs(t *testing.T) (*Inputs, string) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in, err := OpenInputs(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return in, directory
}

func put(t *testing.T, directory, name string, data []byte) {
	t.Helper()
	target := filepath.Join(directory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestInputLimitsAndPaths(t *testing.T) {
	in, dir := testInputs(t)
	put(t, dir, "ok.workflow.txt", []byte(simpleWorkflow))
	data, err := in.Read("ok.workflow.txt", MaxFileBytes)
	if err != nil || string(data) != simpleWorkflow {
		t.Fatalf("read failed: %v", err)
	}
	for _, name := range []string{"../escape", "/absolute", "C:/absolute", "a/../ok.workflow.txt", "a//b", "a\\b", "ok.workflow.txt:stream", "NUL", "COM1.txt", "COM¹.txt", "wild*card", "control\x01", "trailing.", "trailing ", "."} {
		t.Run(name, func(t *testing.T) {
			if _, err := in.Read(name, MaxFileBytes); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
	if _, err := in.Read("missing", MaxFileBytes); err == nil {
		t.Fatal("missing file accepted")
	}
	if err := os.Mkdir(filepath.Join(dir, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Read("directory", MaxFileBytes); err == nil {
		t.Fatal("directory accepted")
	}
	put(t, dir, "large", []byte(strings.Repeat("a", MaxFileBytes+1)))
	if _, err := in.Read("large", MaxFileBytes); err == nil {
		t.Fatal("oversized file accepted")
	}
	put(t, dir, "exact", []byte(strings.Repeat("a", MaxFileBytes)))
	if _, err := in.Read("exact", MaxFileBytes); err != nil {
		t.Fatal(err)
	}
}

func TestInputRejectsSymlinks(t *testing.T) {
	in, dir := testInputs(t)
	put(t, dir, "target", []byte("text"))
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := in.Read("link", MaxFileBytes); err == nil {
		t.Fatal("in-root symlink accepted")
	}
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(dir, "linked-directory")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenInputs(filepath.Join(dir, "linked-directory")); err == nil {
		t.Fatal("symlink root accepted")
	}
	if _, err := in.Read("linked-directory/file", MaxFileBytes); err == nil {
		t.Fatal("symlink parent accepted")
	}
}
