// Package units organizes captured edits using versioned CodeGraph evidence.
package units

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	cg "github.com/compforge/codegraph"
)

// Span is a one-based inclusive source range.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Fragment owns edits exactly once, even when several Units reference it.
// Before and After may contain different kinds or several owners of a replacement.
type Fragment struct {
	BeforeRef  string    `json:"before_ref,omitempty"`
	AfterRef   string    `json:"after_ref,omitempty"`
	ID         string    `json:"id"`
	Path       string    `json:"path"`
	OldPath    string    `json:"old_path,omitempty"`
	Before     []Element `json:"before,omitempty"`
	After      []Element `json:"after,omitempty"`
	Gaps       []string  `json:"gaps,omitempty"`
	Symbols    []string  `json:"symbols,omitempty"`
	Diff       string    `json:"diff"`
	Status     string    `json:"status,omitempty"`
	Insertions int64     `json:"insertions"`
	Deletions  int64     `json:"deletions"`
}

type Relation struct {
	Before       bool       `json:"before"`
	FromFragment string     `json:"from_fragment"`
	ToFragment   string     `json:"to_fragment"`
	Link         Connection `json:"link"`
}

// Unit is a set of references to Fragments, with reasons for grouping and cuts.
type Unit struct {
	Counts         ElementCounts `json:"counts"`
	ID             string        `json:"id"`
	FragmentIDs    []string      `json:"fragment_ids"`
	Paths          []string      `json:"paths"`
	Relations      []Relation    `json:"relations,omitempty"`
	Boundaries     []Relation    `json:"boundaries,omitempty"`
	DiffSize       int           `json:"diff_size"`
	BudgetExceeded bool          `json:"budget_exceeded"`
}

// Options controls grouping, not execution. Zero size limits use defaults;
// MaxUnits=0 imposes no count target. DiffSize defaults to UTF-8 bytes and may
// be supplied by a consumer with a different context accounting unit.
type Options struct {
	// Exclude is called by FormUnits before graph construction or splitting.
	// True excludes the change from its result; nil keeps every change. The
	// callback must not mutate captured source or tags. GroupFragments ignores it.
	Exclude         func(Change) bool
	FileOnly        bool
	MaxUnits        int
	MaxFiles        int
	MaxChangedLines int64
	MaxDiffSize     int
	DiffSize        func(string) int
}

type Step struct {
	Strategy      string `json:"strategy"`
	InputUnits    int    `json:"input_units"`
	OutputUnits   int    `json:"output_units"`
	BudgetBlocked int    `json:"budget_blocked,omitempty"`
}

type Diagnostic struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
	Message  string `json:"message"`
}

type Result struct {
	Fragments     []Fragment       `json:"fragments"`
	Units         []Unit           `json:"units"`
	Relations     []Relation       `json:"relations"`
	Steps         []Step           `json:"steps"`
	Decisions     []Merge          `json:"decisions,omitempty"`
	Merges        []NamespaceMerge `json:"merges,omitempty"`
	Diagnostics   []Diagnostic     `json:"diagnostics"`
	Complete      bool             `json:"complete"`
	LimitExceeded bool             `json:"limit_exceeded"`
}

// Input reuses caller-owned immutable graphs. Each graph and source side must
// describe the same captured version; a missing graph retains file-level edits.
type Input struct {
	Changes       []Change
	Before, After *cg.Graph
	Options       Options
}

// Form splits changes and then groups their unique Fragments.
func Form(ctx context.Context, in Input) (Result, error) {
	var fs []Fragment
	for _, ch := range in.Changes {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		f, err := Split(ctx, ch)
		if err != nil {
			return Result{}, err
		}
		fs = append(fs, f...)
	}
	return Group(ctx, fs, in.Before, in.After, in.Options)
}

// Split uses captured source syntax only. Graphs are consulted during Group.
func Split(ctx context.Context, ch Change) ([]Fragment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var old, next []Element
	var gaps []string
	parse := func(side, path, content string) []Element {
		elements, issues := sourceElements(ctx, path, content)
		for _, issue := range issues {
			gaps = append(gaps, side+": "+issue)
		}
		return elements
	}
	if !ch.IsBinary {
		if !ch.IsNew {
			if ch.OldContentKnown {
				old = parse("before", ch.OldPath, ch.OldFileContent)
			} else {
				gaps = append(gaps, "before: source unavailable")
			}
		}
		if !ch.IsDeleted {
			if !ch.NewContentMissing {
				next = parse("after", ch.NewPath, ch.NewFileContent)
			} else {
				gaps = append(gaps, "after: source unavailable")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fs := splitChange(ch, old, next, gaps)
	if err := ValidateEdits(ch, fs); err != nil {
		return nil, err
	}
	for i := range fs {
		fs[i].BeforeRef = ch.BeforeRef
		fs[i].AfterRef = ch.AfterRef
		fs[i].ID = FragmentID(fs[i])
	}
	return fs, nil
}

func stableID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "-" + hex.EncodeToString(sum[:])[:12]
}

func locationSpan(l cg.Location) Span {
	end := l.EndLine
	if l.EndColumn == 1 && end > l.Line {
		end--
	}
	return Span{l.Line, max(l.Line, end)}
}

func (f Fragment) ChangedSpans(before bool) []Span {
	var out []Span
	for _, h := range ParseHunks(f.Diff) {
		old, next := h.OldStart, h.NewStart
		for _, l := range h.Lines {
			if before && l.Type == HunkDeleted {
				out = append(out, Span{old, old})
			}
			if !before && l.Type == HunkAdded {
				out = append(out, Span{next, next})
			}
			if l.Type != HunkAdded {
				old++
			}
			if l.Type != HunkDeleted {
				next++
			}
		}
	}
	return out
}

func ValidateEdits(ch Change, fragments []Fragment) error {
	counts := func(text string) map[string]int {
		out := map[string]int{}
		for _, h := range ParseHunks(text) {
			old, next := h.OldStart, h.NewStart
			for _, l := range h.Lines {
				if l.Type == HunkAdded {
					out[fmt.Sprintf("+%d:%s", next, l.Content)]++
					next++
				} else if l.Type == HunkDeleted {
					out[fmt.Sprintf("-%d:%s", old, l.Content)]++
					old++
				} else {
					old++
					next++
				}
			}
		}
		return out
	}
	want, got := counts(ch.Diff), map[string]int{}
	for _, f := range fragments {
		for k, n := range counts(f.Diff) {
			got[k] += n
		}
	}
	if len(want) != len(got) {
		return fmt.Errorf("fragment coverage mismatch for %s", ch.Path())
	}
	for k, n := range want {
		if got[k] != n {
			return fmt.Errorf("fragment coverage mismatch for %s at %s", ch.Path(), k)
		}
	}
	return nil
}

func uniqueIDs(ids []string) []string {
	sort.Strings(ids)
	out := ids[:0]
	for _, id := range ids {
		if len(out) == 0 || out[len(out)-1] != id {
			out = append(out, id)
		}
	}
	return out
}
