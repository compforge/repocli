package cmd

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDiffCustomTestPatternsAndHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := fixture(t)
	put(t, repo, "root.check.ts", "import { a } from './source file';\n")
	put(t, repo, "checks/use.check.ts", "import { a } from '../source file';\n")
	put(t, repo, "checks/use.verify.ts", "import { a } from '../source file';\n")
	put(t, repo, "checks/unrelated.check.ts", "export const unrelated = 1;\n")
	gitCommand(t, repo, "add", ".")
	gitCommand(t, repo, "commit", "-qm", "custom tests")
	put(t, repo, "source file.ts", "export function a() { return 9; }\nexport function b() { return 2; }\n")
	cases := []struct{ flags, want []string }{
		{[]string{"--test-pattern", "**/*.check.ts"}, []string{"checks/use.check.ts", "root.check.ts"}},
		{[]string{"--test-dir", "checks", "--test-pattern", "**/*.check.ts", "--test-pattern", "**/*.verify.ts"}, []string{"checks/use.check.ts", "checks/use.verify.ts"}},
		{[]string{"--test-dir", "checks", "--test-pattern", "*.check.ts"}, []string{}},
		{[]string{"--test-pattern", "**/*.{check,verify}.ts"}, []string{"checks/use.check.ts", "checks/use.verify.ts", "root.check.ts"}},
		{[]string{"--test-dir", "."}, []string{"tests/a.test.ts"}},
	}
	for _, tc := range cases {
		report := runJSON(t, append([]string{"diff", "--repo", repo, "--json"}, tc.flags...), "")
		if !reflect.DeepEqual(report.TestFiles, tc.want) {
			t.Fatalf("flags=%v tests=%v want=%v", tc.flags, report.TestFiles, tc.want)
		}
	}
	records := readDiffHistory(t, home)
	if len(records) != len(cases) || !reflect.DeepEqual(records[0].TestPatterns, []string{"**/*.check.ts"}) || !reflect.DeepEqual(records[0].TestDirs, []string{"."}) {
		t.Fatalf("history: %+v", records)
	}
	if len(records[len(records)-1].TestPatterns) != 0 || records[len(records)-1].TestPatterns == nil {
		t.Fatal("defaults must be recorded as an empty array")
	}
}

func TestChangedCustomTestIsDirectEvidenceWithinSearchScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := fixture(t)
	put(t, repo, "checks/own.check.ts", "export function own() { return 1; }\n")
	put(t, repo, "outside/own.check.ts", "export function own() { return 1; }\n")
	gitCommand(t, repo, "add", ".")
	gitCommand(t, repo, "commit", "-qm", "custom tests")
	for _, name := range []string{"checks/own.check.ts", "outside/own.check.ts"} {
		put(t, repo, name, "export function own() { return 2; }\n")
	}
	report := runJSON(t, []string{"diff", "--repo", repo, "--test-dir", "checks", "--test-pattern", "**/*.check.ts", "--json"}, "")
	if !reflect.DeepEqual(report.TestFiles, []string{"checks/own.check.ts"}) {
		t.Fatal(report.TestFiles)
	}
}

func TestInvalidTestPatternIsUsageErrorBeforeAnalysis(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, pattern := range []string{"", "[", "/tests/**", "../tests/**"} {
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), []string{"diff", "--repo", "missing-repository", "--test-pattern", pattern, "--json"}, nil, &out, &stderr)
		if code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "invalid test pattern") {
			t.Fatalf("pattern=%q code=%d out=%s err=%s", pattern, code, out.String(), stderr.String())
		}
	}
}
