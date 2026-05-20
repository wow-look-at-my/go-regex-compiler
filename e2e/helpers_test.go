package e2e_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/go-regex-compiler/internal/codegen"
	"github.com/wow-look-at-my/go-regex-compiler/internal/dfa"
	"github.com/wow-look-at-my/go-regex-compiler/internal/parser"
	"github.com/wow-look-at-my/testify/require"
)

func generateNamed(t *testing.T, pattern, funcName string, mode codegen.MatchMode) []byte {
	t.Helper()
	prog, err := parser.Parse(pattern)
	require.NoError(t, err)
	d, err := dfa.Build(prog)
	require.NoError(t, err)
	var buf bytes.Buffer
	opts := codegen.Options{
		PackageName: "main",
		FuncName:    funcName,
		Regex:       pattern,
		Mode:        mode,
	}
	require.NoError(t, codegen.Generate(&buf, d, opts))
	return buf.Bytes()
}

func generateWithSubmatch(t *testing.T, regex, matchFunc, submatchFunc string) []byte {
	t.Helper()
	result, err := parser.ParseResult(regex)
	require.NoError(t, err)
	d, err := dfa.Build(result.Prog)
	require.NoError(t, err)
	var buf bytes.Buffer
	opts := codegen.Options{
		PackageName: "main",
		FuncName:    matchFunc,
		Regex:       regex,
		Mode:        codegen.MatchFull,
	}
	if result.NumGroups > 0 {
		opts.Submatch = &codegen.SubmatchOptions{
			PackageName: "main",
			FuncName:    submatchFunc,
			MatchFunc:   matchFunc,
			Regex:       regex,
			Prog:        result.Prog,
			NumGroups:   result.NumGroups,
		}
	}
	require.NoError(t, codegen.Generate(&buf, d, opts))
	return buf.Bytes()
}

func writeGenerated(t *testing.T, tmpDir, filename string, src []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, filename), src, 0644))
}

func modInitAndRun(t *testing.T, tmpDir string) {
	t.Helper()

	initCmd := exec.Command("go", "mod", "init", "testmod")
	initCmd.Dir = tmpDir
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "go mod init: %s", out)

	runCmd := exec.Command("go", "run", ".")
	runCmd.Dir = tmpDir
	out, err = runCmd.CombinedOutput()
	require.NoError(t, err, "generated code failed:\n%s", out)
}
