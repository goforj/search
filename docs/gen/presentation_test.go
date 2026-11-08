package gen

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFeatureBadgeKeepsLabelAndArtworkTogether verifies a self-contained accessible SVG and escaped text.
func TestFeatureBadgeKeepsLabelAndArtworkTogether(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "docs/assets/icons/features")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	icon := `<svg viewBox="0 0 24 24"><g stroke="#6a7dff"><path d="M1 1h20"/></g></svg>`
	if err := os.WriteFile(filepath.Join(directory, "test.svg"), []byte(icon), 0600); err != nil {
		t.Fatal(err)
	}
	markdown, err := featureBadge(root, "test", "A & B")
	if err != nil || markdown != "![A & B](docs/assets/badges/feature-test.svg)" {
		t.Fatal(markdown, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs/assets/badges/feature-test.svg"))
	if err != nil {
		t.Fatal(err)
	}
	var badge struct {
		Label  string `xml:"aria-label,attr"`
		Height int    `xml:"height,attr"`
		Title  string `xml:"title"`
	}
	if err := xml.Unmarshal(data, &badge); err != nil || badge.Label != "A & B" || badge.Title != badge.Label || badge.Height != 32 {
		t.Fatal(badge, err)
	}
	if !strings.Contains(string(data), `<path d="M1 1h20"/>`) || strings.Contains(string(data), "<image") {
		t.Fatal("badge does not contain its original vector artwork")
	}
}

// TestFeatureBadgeRejectsInvalidArtwork prevents missing or malformed source assets from producing broken README images.
func TestFeatureBadgeRejectsInvalidArtwork(t *testing.T) {
	root := t.TempDir()
	if _, err := featureBadge(root, "missing", "Missing"); err == nil {
		t.Fatal("missing artwork accepted")
	}
	directory := filepath.Join(root, "docs/assets/icons/features")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"<svg", `<svg viewBox="0 0 16 16"></svg>`} {
		if err := os.WriteFile(filepath.Join(directory, "invalid.svg"), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := featureBadge(root, "invalid", "Invalid"); err == nil {
			t.Fatal("invalid artwork accepted")
		}
	}
}
