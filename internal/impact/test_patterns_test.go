package impact

import (
	"context"
	"reflect"
	"testing"
)

func TestDefaultTestPatterns(t *testing.T) {
	for _, name := range []string{"value_test.go", "lib/value_test.go", "test_value.py", "src/value_test.py", "value.test.ts", "src/value.spec.tsx", "value.test.extra.mjs", "__tests__/value.cts", "src/__tests__/nested/value.jsx"} {
		if !isTest(name, nil) {
			t.Errorf("missed default %q", name)
		}
	}
	for _, name := range []string{"tests/source.go", "test_value.go", "value.py", "test_value.pyi", "value.ts", "value.test.json", "__tests__/readme.md", "__tests__/helper.go"} {
		if isTest(name, nil) {
			t.Errorf("unexpected test %q", name)
		}
	}
}

func TestCustomTestPatternsReplaceDefaultsAndMatchRepositoryPaths(t *testing.T) {
	patterns, err := ValidateTestPatterns([]string{"**/*.check.ts", "specs/**"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"root.check.ts", "src/nested/value.check.ts", "specs/value.py"} {
		if !isTest(name, patterns) {
			t.Errorf("missed %q", name)
		}
	}
	for _, name := range []string{"value.test.ts", "test_value.py", "src/value.go", "specs/data.json"} {
		if isTest(name, patterns) {
			t.Errorf("unexpected test %q", name)
		}
	}
	if isTest("src/value.ts", []string{"*.ts"}) {
		t.Fatal("glob became basename-relative")
	}
	if !isTest("checks/a.verify.ts", []string{"**/*.{check,verify}.ts"}) {
		t.Fatal("brace alternatives lost")
	}
}

func TestValidateTestPatterns(t *testing.T) {
	got, err := ValidateTestPatterns([]string{"./**/*_test.go", "**/*_test.go", "**/test_*.py"})
	if err != nil || !reflect.DeepEqual(got, []string{"**/*_test.go", "**/test_*.py"}) {
		t.Fatalf("%v %v", got, err)
	}
	for _, pattern := range []string{"", ".", "[", "a/[", "{a,b", "/tests/**", "../tests/**", "src/../**", "tests/", "a\\b", "a\x00b"} {
		if _, err := ValidateTestPatterns([]string{pattern}); err == nil {
			t.Errorf("accepted %q", pattern)
		}
	}
	if _, err := Analyze(context.Background(), Request{TestPatterns: []string{"["}}); err == nil {
		t.Fatal("domain accepted invalid glob")
	}
}
