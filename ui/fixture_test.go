package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreeCopiesNestedFilesAndLeavesSourceUntouched(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(src, "scripts"), 0o755)
	os.WriteFile(filepath.Join(src, "config.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(src, "scripts", "a b.json"), []byte("x"), 0o644)
	if err := CopyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"config.json", filepath.Join("scripts", "a b.json")} {
		if _, err := os.Stat(filepath.Join(dst, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	if CopyTree(filepath.Join(src, "nope"), dst) == nil {
		t.Error("missing source must fail")
	}
}
