package project

import (
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"testing"
)

var updateLayouts = flag.Bool("update-inspect-fixtures", false, "record shared inspect expectations")

type layoutCase struct {
	Name     string             `json:"name"`
	Files    map[string]string  `json:"files"`
	Origin   string             `json:"origin"`
	Owners   map[string]*string `json:"owners"`
	Error    bool               `json:"error"`
	Expected json.RawMessage    `json:"expected,omitempty"`
}

func TestInspectConformance(t *testing.T) {
	path := "../../conformance/inspect/layouts.json"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []layoutCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for i := range cases {
		c := &cases[i]
		t.Run(c.Name, func(t *testing.T) {
			files := map[string][]byte{}
			for name, data := range c.Files {
				files[name] = []byte(data)
			}
			actual, err := Load(files, c.Origin)
			if c.Error {
				if err == nil {
					t.Fatal("expected rejection")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(actual)
			if err != nil {
				t.Fatal(err)
			}
			if *updateLayouts {
				c.Expected = data
			}
			var got, want any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(c.Expected, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %s, want %s", data, c.Expected)
			}
			for path, name := range c.Owners {
				owner := actual.Owner(path)
				if name == nil {
					if owner != nil {
						t.Fatalf("unexpected owner for %s", path)
					}
				} else if owner == nil || owner.Name != *name {
					t.Fatalf("incorrect owner for %s", path)
				}
			}
		})
	}
	if *updateLayouts {
		data, err := json.MarshalIndent(cases, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
