// Package cli owns the command-line protocol, not analysis semantics.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repocli/internal/diff"
	"github.com/compforge/repocli/internal/git"
	"github.com/compforge/repocli/internal/impact"
	"github.com/compforge/repocli/internal/project"
)

const usage = `repocli: repository change analysis

Usage:
  repocli diff [--repo DIR] [--base REF] [--test-dir DIR ...] [--json]
  repocli diff --file PATCH|- [--base REF] [--test-dir DIR ...] [--json]

--base defaults to HEAD. It is compared directly to the working tree, including
staged, unstaged and non-ignored untracked files (not a merge-base comparison).
--file reconstructs the postimage from the base in memory; it does not use or
modify working-tree contents. Supply the commit the patch was generated against.
Test directories are relative to the repository root. Without them, only changes
and symbols are reported. Results are static impact estimates, not test verdicts.
`

type Report struct {
	impact.Result
	Checkout   string                    `json:"checkout"`
	Repository *common.Repository        `json:"repository"`
	Base       string                    `json:"base"`
	Input      string                    `json:"input"`
	Components []project.ComponentImpact `json:"components"`
}

type listFlag []string

func (v *listFlag) String() string     { return fmt.Sprint([]string(*v)) }
func (v *listFlag) Set(s string) error { *v = append(*v, s); return nil }

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" || args[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if args[0] != "diff" {
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("repocli diff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repoDir := flags.String("repo", ".", "repository directory")
	base := flags.String("base", "HEAD", "exact base commit/ref")
	patchFile := flags.String("file", "", "Git patch file, or - for stdin")
	asJSON := flags.Bool("json", false, "write structured JSON to stdout")
	timeout := flags.Duration("timeout", 2*time.Minute, "analysis deadline")
	var testDirs listFlag
	flags.Var(&testDirs, "test-dir", "test directory relative to repo root (repeatable)")
	flags.Usage = func() { fmt.Fprint(stderr, usage); flags.PrintDefaults() }
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *timeout <= 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments or non-positive timeout")
		return 2
	}
	dirs, err := impact.ValidateDirs(testDirs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	result, err := analyze(ctx, *repoDir, *base, *patchFile, dirs, stdin)
	if err != nil {
		fmt.Fprintln(stderr, "repocli:", err)
		return 1
	}
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		fmt.Fprintf(stdout, "%d changed files; test scope: %s\n", len(result.Changes), result.Scope)
		for _, component := range result.Components {
			fmt.Fprintf(stdout, "  component %s (%s, %s)\n", component.Component.Name, component.Root, component.Component.Language)
		}
		for _, c := range result.Changes {
			fmt.Fprintf(stdout, "  %s %s\n", c.Status, c.Path)
			for _, s := range c.Before {
				fmt.Fprintf(stdout, "    - %s %s (%d-%d)\n", s.Kind, s.Name, s.StartLine, s.EndLine)
			}
			for _, s := range c.After {
				fmt.Fprintf(stdout, "    + %s %s (%d-%d)\n", s.Kind, s.Name, s.StartLine, s.EndLine)
			}
		}
		for _, reason := range result.Reasons {
			fmt.Fprintf(stdout, "  test %s [%s]\n", reason.TestFile, reason.Kind)
			if len(reason.DependencyPath) > 0 {
				fmt.Fprintf(stdout, "    %v\n", reason.DependencyPath)
			}
		}
		for _, name := range result.SourceFiles {
			fmt.Fprintln(stdout, "  source", name)
		}
		for _, reason := range result.FallbackReasons {
			fmt.Fprintln(stdout, "  fallback:", reason)
		}
	}
	return 0
}

func analyze(ctx context.Context, dir, base, patchFile string, testDirs []string, stdin io.Reader) (Report, error) {
	r, err := git.Open(ctx, dir)
	if err != nil {
		return Report{}, err
	}
	ref, err := r.Resolve(ctx, base)
	if err != nil {
		return Report{}, err
	}
	before, err := r.Base(ctx, ref)
	if err != nil {
		return Report{}, err
	}
	var after git.Snapshot
	var changes []diff.Change
	input := "working_tree"
	if patchFile != "" {
		input = "patch"
		reader := stdin
		if patchFile != "-" {
			f, err := os.Open(patchFile)
			if err != nil {
				return Report{}, err
			}
			defer f.Close()
			reader = f
		}
		changes, err = diff.Parse(reader)
		if err != nil {
			return Report{}, err
		}
		after.Files, err = diff.Apply(before.Files, changes)
		if err != nil {
			return Report{}, err
		}
	} else {
		patch, err := r.Patch(ctx, ref)
		if err != nil {
			return Report{}, err
		}
		changes, err = diff.Parse(bytes.NewReader(patch))
		if err != nil {
			return Report{}, err
		}
		var untracked []string
		after, untracked, err = r.Working(ctx)
		if err != nil {
			return Report{}, err
		}
		changed := map[string]bool{}
		for _, c := range changes {
			changed[c.Path] = true
		}
		for _, name := range untracked {
			if !changed[name] {
				changes = append(changes, diff.Added(name, after.Files[name]))
			}
		}
		sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	}
	issues := append(before.Issues, after.Issues...)
	result, err := impact.Analyze(ctx, impact.Request{Before: before.Files, After: after.Files, Changes: changes, TestDirs: testDirs, Issues: issues})
	if err != nil {
		return Report{}, err
	}
	origin, err := r.Origin(ctx)
	if err != nil {
		return Report{}, err
	}
	oldLayout, err := project.Load(before.Files, origin)
	if err != nil {
		return Report{}, err
	}
	newLayout, err := project.Load(after.Files, origin)
	if err != nil {
		return Report{}, err
	}
	return Report{Result: result, Checkout: r.Root, Repository: newLayout.Repository, Base: ref, Input: input,
		Components: project.Group(oldLayout, newLayout, changes, result.SourceFiles, result.TestFiles)}, nil
}
