package impact

import (
	"context"
	"slices"
	"testing"

	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/diff"
)

// +spec=`A local outline omission does not downgrade unrelated impact evidence`
func TestOutlineGapRetainsEvidenceWithoutGlobalDowngrade(t *testing.T) {
	base := map[string]string{
		"src/value.ts":        "export function value() { return 1; }\n",
		"src/agent.ts":        "export class Agent { private async *retry(text: string): AsyncGenerator<Event> {} }\n",
		"tests/value.test.ts": "import { value } from '../src/value';\nimport { Agent } from '../src/agent';\n",
	}
	r := runChange(t, base, "src/value.ts", "return 1", "return 2")
	if r.Scope != "focused" || len(r.FallbackReasons) != 0 || !slices.Equal(r.TestFiles, []string{"tests/value.test.ts"}) {
		t.Fatalf("unrelated gap broadened result: %+v", r)
	}
	count := 0
	for _, observation := range r.Observations {
		if observation.Reason != "outline_incomplete" {
			continue
		}
		count++
		if observation.Path != "src/agent.ts" || observation.Subject != "declarations" || observation.Outline == nil || observation.Location.Path != observation.Path {
			t.Fatalf("lost local coverage evidence: %+v", observation)
		}
	}
	if count != 2 {
		t.Fatalf("want one omission per snapshot, got %d: %+v", count, r.Observations)
	}
	r = runChange(t, base, "src/agent.ts", "text: string", "text: number")
	for _, seed := range r.Seeds {
		if seed.Granularity != "file" || seed.Basis != "declaration_gap" {
			t.Fatalf("unsafe declaration seed: %+v", seed)
		}
	}
	if len(r.Seeds) != 2 || len(r.TestFiles) != 1 {
		t.Fatalf("missing widened seeds: %+v", r)
	}
}

// +spec=`Diff returns affected files without requiring test directories`
func TestAffectedFilesWithoutTests(t *testing.T) {
	before := files(map[string]string{
		"source.ts":    "export function value() { return 1; }\nexport function other() { return 2; }\n",
		"consumer.ts":  "import {value} from './source';\n",
		"unrelated.ts": "import {other} from './source';\n",
	})
	after := files(map[string]string{
		"source.ts":   "export function value() { return 3; }\nexport function other() { return 2; }\n",
		"consumer.ts": string(before["consumer.ts"]), "unrelated.ts": string(before["unrelated.ts"]),
	})
	r, err := Analyze(context.Background(), Request{Before: before, After: after, Changes: []diff.Change{{Path: "source.ts", Status: "modified", Hunks: []diff.Hunk{{Old: diff.Range{Start: 1, Count: 1}, New: diff.Range{Start: 1, Count: 1}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range r.AffectedFiles {
		paths = append(paths, file.Path)
	}
	if !slices.Equal(paths, []string{"consumer.ts", "source.ts"}) || len(r.TestFiles) != 0 || r.Scope != "focused" {
		t.Fatalf("%+v", r)
	}
	consumer := r.AffectedFiles[0]
	if consumer.Confidence != codegraph.Exact || consumer.Distance != 1 || consumer.Seed.Granularity != "symbol" || consumer.Seed.ID != codegraph.SymbolID("source.ts", "value") || consumer.Version != consumer.Seed.Version {
		t.Fatalf("missing evidence policy: %+v", consumer)
	}
}

func TestSeedsUseEachSnapshotsDeclarations(t *testing.T) {
	base := map[string]string{
		"src/source.ts":     "export function oldName() { return 1; }\n",
		"tests/old.test.ts": "import {oldName} from '../src/source';\n",
		"tests/new.test.ts": "import {newName} from '../src/source';\n",
	}
	r := runChange(t, base, "src/source.ts", "oldName", "newName")
	for _, seed := range r.Seeds {
		if seed.Granularity != "symbol" {
			continue
		}
		want := "oldName"
		if seed.Version == "after" {
			want = "newName"
		}
		if seed.ID != codegraph.SymbolID("src/source.ts", want) {
			t.Fatalf("mixed snapshot seeds: %+v", r.Seeds)
		}
	}
	for _, reason := range r.Reasons {
		want := "before"
		if reason.TestFile == "tests/new.test.ts" {
			want = "after"
		}
		if reason.Version != want || reason.Seed.Version != want {
			t.Fatalf("mixed snapshot evidence: %+v", reason)
		}
	}
	if len(r.TestFiles) != 2 {
		t.Fatalf("%+v", r)
	}
}
