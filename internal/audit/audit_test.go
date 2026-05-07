package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseAge(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Time
	}{
		{"2y", now.AddDate(-2, 0, 0)},
		{"6m", now.AddDate(0, -6, 0)},
		{"3w", now.AddDate(0, 0, -21)},
		{"90d", now.AddDate(0, 0, -90)},
	}

	for _, tt := range tests {
		got, err := ParseAge(tt.in, now)
		if err != nil {
			t.Fatalf("ParseAge(%q) returned error: %v", tt.in, err)
		}
		if !got.Equal(tt.want) {
			t.Fatalf("ParseAge(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestDiscoverProjectRoots(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "ProjectA"))
	mustMkdir(t, filepath.Join(root, "ProjectB"))
	mustMkdir(t, filepath.Join(root, "Archived Projects", "LegacyOne"))
	mustMkdir(t, filepath.Join(root, "Archived Projects", "LegacyTwo"))

	roots, err := DiscoverProjectRoots(root)
	if err != nil {
		t.Fatalf("DiscoverProjectRoots() error = %v", err)
	}
	if len(roots) != 4 {
		t.Fatalf("expected 4 roots, got %d", len(roots))
	}

	labels := map[string]bool{}
	for _, r := range roots {
		labels[r.Label] = true
	}
	for _, label := range []string{"ProjectA", "ProjectB", "[Archived] LegacyOne", "[Archived] LegacyTwo"} {
		if !labels[label] {
			t.Fatalf("missing label %q", label)
		}
	}
}

func TestFormatAge(t *testing.T) {
	if got := FormatAge(0); got != "0d" {
		t.Fatalf("expected 0d, got %q", got)
	}
	span := 400 * 24 * time.Hour
	if got := FormatAge(span); got != "1y 1m 5d" {
		t.Fatalf("expected 1y 1m 5d, got %q", got)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
