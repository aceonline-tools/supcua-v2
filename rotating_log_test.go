package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriterRotatesAtThreshold(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	w, err := newRotatingWriter(p, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	line := strings.Repeat("a", 40)
	for i := 0; i < 5; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	cur, _ := os.ReadFile(p)
	old, err := os.ReadFile(p + ".1")
	if err != nil {
		t.Fatalf("no backup: %v", err)
	}
	if len(cur) != 40 || len(old) != 80 {
		t.Fatalf("sizes cur=%d old=%d, want 40/80", len(cur), len(old))
	}
}

func TestRotatingWriterTruncatesOnOpen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	os.WriteFile(p, []byte(strings.Repeat("b", 500)), 0644)
	w, err := newRotatingWriter(p, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Write([]byte("hi"))
	if b, _ := os.ReadFile(p); string(b) != "hi" {
		t.Fatalf("current = %q", b)
	}
	if _, err := os.Stat(p + ".1"); err == nil {
		t.Fatal("unexpected backup on open")
	}
}
