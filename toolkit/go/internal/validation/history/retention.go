package history

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tidwall/wal"
)

// Retention removes only a prefix closed over Run references. A recent reuse or
// an unfinished run pins its start; references in the retained suffix also pin
// their starts. This preserves replay semantics instead of leaving orphan events.
func (h *Store) Maintain(ctx context.Context, days int) (*State, error) {
	var state *State
	err := h.access(ctx, func(log *wal.Log) error {
		var err error
		state, err = h.replay(ctx, log)
		if err != nil {
			return err
		}
		first, err := log.FirstIndex()
		if err != nil {
			return err
		}
		last, err := log.LastIndex()
		if err != nil {
			return err
		}
		cutoff := time.Now().UTC().AddDate(0, 0, -days)
		keep := last + 1
		for _, run := range state.Runs {
			if run.Result == nil || !run.LastAt.Before(cutoff) {
				keep = min(keep, run.Index)
			}
		}
		for {
			previous := keep
			for _, run := range state.Runs {
				if run.LastIndex >= keep {
					keep = min(keep, run.Index)
				}
			}
			if keep == previous {
				break
			}
		}
		if keep > first {
			if err := log.TruncateFront(keep); err != nil {
				return err
			}
			state, err = h.replay(ctx, log)
			if err != nil {
				return err
			}
		}
		for _, kind := range []string{"inputs", "logs"} {
			entries, err := os.ReadDir(filepath.Join(h.dir, kind))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				id := entry.Name()
				if kind == "inputs" {
					id = strings.TrimSuffix(id, ".json")
				}
				if !validID(id) {
					continue
				}
				if kind == "inputs" && state.Inputs[id] || kind == "logs" && state.Runs[id] != nil {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				// Fresh orphan files may belong to a concurrent validation between capture
				// and started/finished. Do not collect them before the retention window.
				if info.ModTime().Before(cutoff) {
					if err := os.RemoveAll(filepath.Join(h.dir, kind, entry.Name())); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	return state, err
}
