package units

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSplitWithoutGraphClassifiesSource(t *testing.T) {
	for _, tc := range []struct {
		path, source string
		want         []Kind
	}{
		{"types.go", "package p\ntype User struct { Name string }\ntype Store interface { Save(User) error }\nfunc New() User {return User{}}\n", []Kind{Struct, Interface, Function}},
		{"types.ts", "export interface Store { save(): void }\nexport class User { name: string }\n", []Kind{Interface, Class}},
		{"types.py", "class User:\n    pass\ndef save():\n    pass\n", []Kind{Class, Function}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			fs, err := Split(context.Background(), added(tc.path, tc.source))
			if err != nil {
				t.Fatal(err)
			}
			kinds := map[Kind]bool{}
			for _, f := range fs {
				for kind := range f.Counts().After {
					kinds[kind] = true
				}
			}
			for _, kind := range tc.want {
				if !kinds[kind] {
					t.Fatalf("missing %s in %+v", kind, fs)
				}
			}
			b, err := json.Marshal(fs)
			if err != nil {
				t.Fatal(err)
			}
			for _, graphField := range []string{"node_id", "snapshot", "symbol_id"} {
				if strings.Contains(string(b), graphField) {
					t.Fatalf("graph field leaked: %s", b)
				}
			}
		})
	}
}

func TestUnknownFragmentsCoalesceOnlyWithinFile(t *testing.T) {
	fs := []Fragment{
		{Path: "a.txt", Diff: "@@ -1 +1 @@\n-a\n+b\n"},
		{Path: "a.txt", Diff: "@@ -9 +9 @@\n-c\n+d\n"},
		{Path: "b.txt", Diff: "@@ -1 +1 @@\n-e\n+f\n"},
	}
	r, err := Group(context.Background(), fs, nil, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 {
		t.Fatalf("unknown groups=%+v", r.Units)
	}
	for _, u := range r.Units {
		if len(u.Paths) != 1 {
			t.Fatal(u)
		}
		if u.Paths[0] == "a.txt" && len(u.FragmentIDs) != 2 {
			t.Fatal(u)
		}
	}
}

func TestMetadataOnlyIsUnknownAndPreservesStatus(t *testing.T) {
	fs, err := Split(context.Background(), Change{OldPath: "a", NewPath: "b", IsRenamed: true, OldContentKnown: true})
	if err != nil || len(fs) != 1 || !fs[0].Counts().Only(Unknown) || fs[0].Status != "renamed" {
		t.Fatal(fs, err)
	}
}
