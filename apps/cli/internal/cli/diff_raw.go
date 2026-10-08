package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/compforge/repocli/toolkit/go"
)

func writeRawDiff(w io.Writer, d repocli.DiffReport, structured bool) error {
	if structured {
		e := json.NewEncoder(w)
		e.SetIndent("", "  ")
		return e.Encode(d)
	}
	for _, ch := range d.Changes {
		if _, err := fmt.Fprintf(w, "diff %s (+%d -%d)\n%s\n", ch.Path(), ch.Insertions, ch.Deletions, ch.Diff); err != nil {
			return err
		}
	}
	for _, diagnostic := range d.Diagnostics {
		if _, err := fmt.Fprintf(w, "[%s] %s: %s\n", diagnostic.Code, diagnostic.Path, diagnostic.Message); err != nil {
			return err
		}
	}
	return nil
}
