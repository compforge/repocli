package repocli_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/compforge/repocli/toolkit/go"
)

func TestDeadcodeFullGraphAndSelectedSnapshot(t *testing.T) {
	root := fixture(t)
	put(t, root, "main.go", "package main\nfunc main() { used() }\nfunc used() {}\nfunc orphan() {}\n")
	git(t, root, "add", "main.go")
	git(t, root, "commit", "-qm", "declarations")
	put(t, root, "caller.go", "package main\nfunc stagedCaller() { orphan() }\n")
	git(t, root, "add", "caller.go")
	put(t, root, "caller.go", "package main\nfunc workingCaller() { orphan() }\n")
	for _, tc := range []struct {
		input string
		req   repocli.InputRequest
		want  []string
	}{
		{"commit", repocli.InputRequest{Repository: root, Head: "HEAD"}, []string{"main", "orphan"}},
		{"index", repocli.InputRequest{Repository: root, Staged: true}, []string{"stagedCaller", "main"}},
		{"working_tree", repocli.InputRequest{Repository: root}, []string{"workingCaller", "main"}},
	} {
		report, err := repocli.AnalyzeDeadcode(context.Background(), repocli.DeadcodeRequest{InputRequest: tc.req})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, node := range report.Nodes {
			names = append(names, node.Name)
		}
		if !reflect.DeepEqual(names, tc.want) {
			t.Fatalf("%s: %v", tc.input, names)
		}
		if report.SchemaVersion != 1 || report.Snapshot.Input != tc.input || !report.Snapshot.Complete || report.Diagnostics == nil {
			t.Fatalf("%+v", report)
		}
	}
	// A caller beyond the viewer's default 2,000-document window must count.
	for i := 0; i < 2001; i++ {
		put(t, root, fmt.Sprintf("padding/%04d.txt", i), "")
	}
	put(t, root, "zz.go", "package main\nvar callback = workingCaller\n")
	report, err := repocli.AnalyzeDeadcode(context.Background(), repocli.DeadcodeRequest{InputRequest: repocli.InputRequest{Repository: root}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Documents <= 2000 {
		t.Fatal(report.Documents)
	}
	for _, node := range report.Nodes {
		if node.Name == "workingCaller" {
			t.Fatal("omitted late caller")
		}
	}
}

func TestDeadcodeRetainsGraphDiagnostics(t *testing.T) {
	root := fixture(t)
	put(t, root, "main.go", "package main\nfunc main() { missing() }\n")
	report, err := repocli.AnalyzeDeadcode(context.Background(), repocli.DeadcodeRequest{InputRequest: repocli.InputRequest{Repository: root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) == 0 || !report.Snapshot.Complete {
		t.Fatalf("%+v", report)
	}
	if _, err := repocli.AnalyzeDeadcode(context.Background(), repocli.DeadcodeRequest{InputRequest: repocli.InputRequest{Repository: root, Head: "HEAD", Staged: true}}); err == nil {
		t.Fatal("accepted conflicting inputs")
	}
}

func TestDeadcodeBuildBudgets(t *testing.T) {
	root := fixture(t)
	for _, req := range []repocli.DeadcodeRequest{
		{MaxNodes: -1}, {MaxRelations: -1},
		{InputRequest: repocli.InputRequest{Repository: root}, MaxNodes: 1},
		{InputRequest: repocli.InputRequest{Repository: root}, MaxRelations: 1},
	} {
		if _, err := repocli.AnalyzeDeadcode(context.Background(), req); err == nil {
			t.Fatalf("accepted exhausted/invalid budget: %+v", req)
		}
	}
}
