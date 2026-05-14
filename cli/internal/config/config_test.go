package config

import (
	"path/filepath"
	"testing"
)

func TestResolveDirUsesHomeDirFlagBeforeEnv(t *testing.T) {
	envRoot := t.TempDir()
	flagRoot := t.TempDir()
	t.Setenv("CHANWIRE_DIR", envRoot)

	got, err := ResolveDir(flagRoot)
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	want := filepath.Join(flagRoot, ".config", "chanwire")
	if got != want {
		t.Fatalf("ResolveDir: got %q want %q", got, want)
	}
}

func TestResolveDirNormalizesConfigDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHANWIRE_DIR", root)

	got, err := ResolveDir("")
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	want := filepath.Join(root, ".config", "chanwire")
	if got != want {
		t.Fatalf("ResolveDir: got %q want %q", got, want)
	}
}

func TestResolveDirAppendsChanwireToConfigDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHANWIRE_DIR", filepath.Join(root, ".config"))

	got, err := ResolveDir("")
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	want := filepath.Join(root, ".config", "chanwire")
	if got != want {
		t.Fatalf("ResolveDir: got %q want %q", got, want)
	}
}

func TestResolveDirKeepsConfigChanwireDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CHANWIRE_DIR", filepath.Join(root, ".config", "chanwire"))

	got, err := ResolveDir("")
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	want := filepath.Join(root, ".config", "chanwire")
	if got != want {
		t.Fatalf("ResolveDir: got %q want %q", got, want)
	}
}

func TestResolveDirFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHANWIRE_DIR", "")
	t.Setenv("HOME", home)

	got, err := ResolveDir("")
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	want := filepath.Join(home, ".config", "chanwire")
	if got != want {
		t.Fatalf("ResolveDir: got %q want %q", got, want)
	}
}

func TestResolveDirRejectsRelativeHomeDirWithParentTraversal(t *testing.T) {
	if _, err := ResolveDir(filepath.Join("..", "other")); err == nil {
		t.Fatal("expected parent traversal error, got nil")
	}
	if _, err := ResolveDir("child/../other"); err == nil {
		t.Fatal("expected nested parent traversal error, got nil")
	}
}
