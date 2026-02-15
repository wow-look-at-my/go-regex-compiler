package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunToStdout(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := run("[a-z]+", "mypkg", "MatchLower", "")
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("run() error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "package mypkg") {
		t.Error("output missing package declaration")
	}
	if !strings.Contains(output, "func MatchLower(input string) bool") {
		t.Error("output missing function declaration")
	}
}

func TestRunToFile(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "match.go")

	err := run("[0-9]+", "testpkg", "MatchDigits", outFile)
	if err != nil {
		t.Fatalf("run() error: %v", err)
	}

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	output := string(content)
	if !strings.Contains(output, "package testpkg") {
		t.Error("output missing package declaration")
	}
	if !strings.Contains(output, "func MatchDigits(input string) bool") {
		t.Error("output missing function declaration")
	}
}

func TestRunMissingRegex(t *testing.T) {
	err := run("", "pkg", "Match", "")
	if err == nil {
		t.Error("expected error for missing regex")
	}
}

func TestRunInvalidRegex(t *testing.T) {
	err := run("[unclosed", "pkg", "Match", "")
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestRunDefaultPackage(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := run("abc", "", "Match", "")
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("run() error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Should default to "main" when GOPACKAGE is not set
	if !strings.Contains(output, "package main") {
		t.Error("should default to package main")
	}
}

func TestRunGOPACKAGEEnv(t *testing.T) {
	t.Setenv("GOPACKAGE", "envpkg")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := run("abc", "", "Match", "")
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("run() error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "package envpkg") {
		t.Error("should use GOPACKAGE env var")
	}
}

func TestRunInvalidOutputPath(t *testing.T) {
	err := run("abc", "pkg", "Match", "/nonexistent/dir/file.go")
	if err == nil {
		t.Error("expected error for invalid output path")
	}
}
