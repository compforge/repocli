package validation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/compforge/repokit/toolkit/go/internal/git"
	"github.com/compforge/repokit/toolkit/go/internal/project"
	"github.com/tidwall/wal"
)

// HistoryCounts counts real attempts separately from reuse. Seconds never includes
// the original run duration a second time when its result is reused.
type HistoryCounts struct {
	Executions       int       `json:"executions"`
	Passed           int       `json:"passed"`
	Failed           int       `json:"failed"`
	Unfinished       int       `json:"unfinished"`
	Other            int       `json:"other"`
	Reuses           int       `json:"reuses"`
	ExecutionSeconds float64   `json:"executionSeconds"`
	LookupSeconds    float64   `json:"lookupSeconds"`
	LastCheckedAt    time.Time `json:"lastCheckedAt,omitempty"`
}

// ValidationHistory describes retained records, not lifetime totals.
type ValidationHistory struct {
	Repository string                   `json:"repository"`
	From       time.Time                `json:"from"`
	To         time.Time                `json:"to"`
	Checks     map[string]HistoryCounts `json:"checks"`
}

type historyEvent struct {
	SchemaVersion int         `json:"schemaVersion"`
	Type          string      `json:"type"`
	Checkout      string      `json:"checkout"`
	At            time.Time   `json:"at"`
	Run           string      `json:"run"`
	Component     string      `json:"component"`
	Inputs        string      `json:"inputs"`
	Check         CheckResult `json:"check"`
	Consistent    bool        `json:"inputsConsistent,omitempty"`
}

type historyRun struct {
	Start      historyEvent
	Result     *CheckResult
	Consistent bool
	Index      uint64
	LastIndex  uint64
	LastAt     time.Time
}

type historyState struct {
	Runs    map[string]*historyRun
	Order   []string
	Summary ValidationHistory
	Inputs  map[string]bool
}

func newHistoryState(repository string) *historyState {
	return &historyState{Runs: map[string]*historyRun{}, Inputs: map[string]bool{}, Summary: ValidationHistory{Repository: repository, Checks: map[string]HistoryCounts{}}}
}

// +spec=Replay and live writes use the same projection; unfinished runs never grant evidence
func (s *historyState) apply(e historyEvent, index uint64) error {
	if e.SchemaVersion != 2 || e.Checkout == "" || e.Run == "" || e.At.IsZero() || (e.Check.Check != "lint" && e.Check.Check != "test") {
		return errors.New("invalid validation history event")
	}
	key := e.Component + " " + e.Check.Check
	counts := s.Summary.Checks[key]
	switch e.Type {
	case "started":
		if s.Runs[e.Run] != nil {
			return fmt.Errorf("duplicate run %s", e.Run)
		}
		s.Runs[e.Run] = &historyRun{Start: e, Index: index}
		s.Order = append(s.Order, e.Run)
		counts.Executions++
		counts.Unfinished++
	case "finished":
		run := s.Runs[e.Run]
		if run == nil || run.Result != nil || run.Start.Checkout != e.Checkout || run.Start.Component != e.Component || run.Start.Check.Check != e.Check.Check || run.Start.Inputs != e.Inputs {
			return fmt.Errorf("invalid completion for run %s", e.Run)
		}
		result := e.Check
		run.Result, run.Consistent = &result, e.Consistent
		counts.Unfinished--
		counts.ExecutionSeconds += result.DurationSeconds
		counts.LookupSeconds += result.LookupSeconds
		if result.CheckedAt.After(counts.LastCheckedAt) {
			counts.LastCheckedAt = result.CheckedAt
		}
		switch result.Status {
		case "passed":
			counts.Passed++
		case "failed":
			counts.Failed++
		default:
			counts.Other++
		}
	case "reused":
		run := s.Runs[e.Run]
		if run == nil || run.Result == nil || !run.Consistent || run.Result.Status != "passed" || run.Start.Checkout != e.Checkout || run.Start.Component != e.Component || run.Start.Check.Check != e.Check.Check {
			return fmt.Errorf("invalid reuse of run %s", e.Run)
		}
		counts.Reuses++
		counts.LookupSeconds += e.Check.LookupSeconds
	default:
		return fmt.Errorf("unknown history event %q", e.Type)
	}
	run := s.Runs[e.Run]
	run.LastIndex, run.LastAt = index, e.At
	s.Inputs[e.Inputs] = true
	s.Summary.Checks[key] = counts
	if s.Summary.From.IsZero() || e.At.Before(s.Summary.From) {
		s.Summary.From = e.At
	}
	if e.At.After(s.Summary.To) {
		s.Summary.To = e.At
	}
	return nil
}

type historyStore struct {
	dir         string
	checkout    string
	repository  string
	segmentSize int
}

func newHistoryStore(base, repository, checkout string) *historyStore {
	return &historyStore{dir: filepath.Join(base, repository), repository: repository, checkout: checkout}
}

// Repository naming reuses the credential-free origin identity. Local repositories
// use shared Git metadata so linked worktrees still share one store.
func openHistoryStore(ctx context.Context, base, checkout string) (*historyStore, error) {
	repo, err := git.Open(ctx, checkout)
	if err != nil {
		return nil, err
	}
	origin, err := repo.Origin(ctx)
	if err != nil {
		return nil, err
	}
	var name string
	if identity := project.FromOrigin(origin); identity != nil {
		parts := append([]string{identity.Forge.Name}, strings.Split(identity.Path, "/")...)
		for i, part := range parts {
			parts[i] = historyNamePart(part)
		}
		name = strings.Join(parts, "--")
	} else {
		common, err := repo.CommonDir(ctx)
		if err != nil {
			return nil, err
		}
		label := filepath.Base(common)
		if label == ".git" {
			label = filepath.Base(filepath.Dir(common))
		}
		digest := sha256.Sum256([]byte(common))
		name = fmt.Sprintf("local--%s--%x", historyNamePart(label), digest[:8])
	}
	return newHistoryStore(base, name, checkout), nil
}

func historyNamePart(part string) string {
	// Escape the separator as well as path characters to keep distinct identities distinct.
	return strings.ReplaceAll(url.PathEscape(part), "-", "%2D")
}

// WAL caches file offsets and sequence numbers. Hold the OS lock over its entire
// open/read/write/close lifetime, never across a Make invocation.
// +spec=Independent CLI processes serialize history access without serializing checks
func (h *historyStore) access(ctx context.Context, fn func(*wal.Log) error) (err error) {
	if err = os.MkdirAll(h.dir, 0700); err != nil {
		return err
	}
	unlock, err := lockHistory(ctx, filepath.Join(h.dir, "lock"))
	if err != nil {
		return err
	}
	defer unlock()
	log, err := wal.Open(filepath.Join(h.dir, "history"), &wal.Options{LogFormat: wal.JSON, SegmentSize: h.segmentSize, AllowEmpty: true, DirPerms: 0700, FilePerms: 0600})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, log.Close()) }()
	return fn(log)
}

func (h *historyStore) replay(ctx context.Context, log *wal.Log) (*historyState, error) {
	state := newHistoryState(h.repository)
	first, err := log.FirstIndex()
	if err != nil {
		return nil, err
	}
	last, err := log.LastIndex()
	if err != nil {
		return nil, err
	}
	for index := first; index <= last; index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := log.Read(index)
		if err != nil {
			return nil, err
		}
		var e historyEvent
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("history entry %d: %w", index, err)
		}
		if err := state.apply(e, index); err != nil {
			return nil, fmt.Errorf("history entry %d: %w", index, err)
		}
	}
	return state, nil
}

func (h *historyStore) read(ctx context.Context) (*historyState, error) {
	var state *historyState
	err := h.access(ctx, func(log *wal.Log) error {
		var err error
		state, err = h.replay(ctx, log)
		return err
	})
	return state, err
}

func (h *historyStore) append(ctx context.Context, events []historyEvent) error {
	if len(events) == 0 {
		return nil
	}
	// +spec=Validate the complete batch against current state under the WAL write lock
	return h.access(ctx, func(log *wal.Log) error {
		state, err := h.replay(ctx, log)
		if err != nil {
			return err
		}
		index, err := log.LastIndex()
		if err != nil {
			return err
		}
		batch := new(wal.Batch)
		for _, e := range events {
			// Retention may remove a selected run before this session finishes.
			// Its result remains valid, but its reuse cannot outlive its history.
			if e.Type == "reused" && state.Runs[e.Run] == nil {
				slog.WarnContext(ctx, "validation reuse history expired", "checkout", h.checkout, "run", e.Run)
				continue
			}
			if err := state.apply(e, index+1); err != nil {
				return fmt.Errorf("append validation history: %w", err)
			}
			data, err := json.Marshal(e)
			if err != nil {
				return err
			}
			index++
			batch.Write(index, data)
		}
		return log.WriteBatch(batch)
	})
}

func atomicHistoryFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (h *historyStore) saveInputs(id string, manifest inputManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return atomicHistoryFile(filepath.Join(h.dir, "inputs", id+".json"), data)
}

func (h *historyStore) loadInputs(id string) (inputManifest, error) {
	var m inputManifest
	if !validHistoryID(id) {
		return m, errors.New("invalid input record ID")
	}
	data, err := os.ReadFile(filepath.Join(h.dir, "inputs", id+".json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.SchemaVersion != 1 || m.Checkout != h.checkout || m.Files == nil {
		return m, errors.New("invalid input record")
	}
	return m, nil
}

func validHistoryID(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if !(c >= 'A' && c <= 'Z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

func (h *historyStore) saveOutput(result CheckResult) error {
	if err := atomicHistoryFile(filepath.Join(h.dir, "logs", result.RunID, "stdout"), []byte(result.Stdout)); err != nil {
		return err
	}
	return atomicHistoryFile(filepath.Join(h.dir, "logs", result.RunID, "stderr"), []byte(result.Stderr))
}

func (h *historyStore) loadOutput(result *CheckResult) error {
	if !validHistoryID(result.RunID) {
		return errors.New("invalid run ID")
	}
	out, err := os.ReadFile(filepath.Join(h.dir, "logs", result.RunID, "stdout"))
	if err != nil {
		return err
	}
	stderr, err := os.ReadFile(filepath.Join(h.dir, "logs", result.RunID, "stderr"))
	if err != nil {
		return err
	}
	result.Stdout, result.Stderr = string(out), string(stderr)
	return nil
}

// ReadValidationHistory reads retained execution statistics without executing checks.
func ReadValidationHistory(ctx context.Context, directory, checkout string) (ValidationHistory, error) {
	checkout, err := filepath.Abs(checkout)
	if err != nil {
		return ValidationHistory{}, err
	}
	checkout, err = filepath.EvalSymlinks(checkout)
	if err != nil {
		return ValidationHistory{}, err
	}
	if directory == "" {
		return ValidationHistory{}, errors.New("history directory is required")
	}
	store, err := openHistoryStore(ctx, directory, checkout)
	if err != nil {
		return ValidationHistory{}, err
	}
	state, err := store.read(ctx)
	if err != nil {
		return ValidationHistory{}, err
	}
	return state.Summary, nil
}

type historySession struct {
	mu          sync.Mutex
	store       *historyStore
	state       *historyState
	manifests   map[string]inputManifest
	diagnostics []string
	failed      bool
	inputID     string
	manifest    inputManifest
	force       bool
}

func beginHistory(ctx context.Context, plan ValidationPlan) *historySession {
	s := &historySession{state: newHistoryState(""), manifests: map[string]inputManifest{}, inputID: plan.InputID, manifest: plan.manifest, force: plan.force}
	if plan.historyDir == "" {
		return s
	}
	var err error
	s.store, err = openHistoryStore(ctx, plan.historyDir, plan.Checkout)
	if err != nil {
		s.fail(ctx, err)
		return s
	}
	state, err := s.store.maintain(ctx, plan.retentionDays)
	if err == nil {
		err = s.store.saveInputs(plan.InputID, plan.manifest)
	}
	if err != nil {
		s.fail(ctx, err)
		return s
	}
	s.state = state
	return s
}

func (s *historySession) fail(ctx context.Context, err error) {
	s.failed = true
	message := "validation history unavailable: " + err.Error()
	s.diagnostics = append(s.diagnostics, message)
	slog.WarnContext(ctx, "validation history unavailable", "error", err)
}

func (s *historySession) candidate(root string, spec CheckSpec) *historyRun {
	for _, id := range slices.Backward(s.state.Order) {
		run := s.state.Runs[id]
		if run.Start.Checkout == s.manifest.Checkout && run.Start.Component == root && run.Start.Check.Check == spec.Check && run.Result != nil && run.Result.Status == "passed" && run.Consistent && !run.Result.CheckedAt.IsZero() && !run.Start.Check.Inputs.DisableReuse {
			return run
		}
	}
	return nil
}

// +spec=Successful history grants reuse only for the same task, inputs and environment
// +spec=History never chooses or widens affected-file execution scope
func (s *historySession) reuse(ctx context.Context, root string, spec CheckSpec) (*CheckResult, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.force {
		return nil, "forced execution"
	}
	if spec.Inputs.DisableReuse {
		return nil, "reuse disabled by caller"
	}
	if s.failed {
		return nil, "history unavailable"
	}
	if s.store == nil {
		return nil, "history disabled"
	}
	run := s.candidate(root, spec)
	if run == nil {
		return nil, "no previous successful check"
	}
	previous := *run.Result
	if previous.Environment != spec.Environment {
		return nil, "execution environment changed"
	}
	if previous.Scope != spec.Scope || !slices.Equal(previous.Command, spec.Command) || !slices.Equal(previous.Files, spec.Files) {
		return nil, "command or scope changed"
	}
	if !reflect.DeepEqual(previous.Witnesses, spec.Witnesses) || (!slices.Equal(previous.Inputs.Components, spec.Inputs.Components) || !slices.Equal(previous.Inputs.Paths, spec.Inputs.Paths)) {
		return nil, "input declaration or receipt changed"
	}
	manifest, ok := s.manifests[run.Start.Inputs]
	if !ok {
		var err error
		manifest, err = s.store.loadInputs(run.Start.Inputs)
		if err != nil {
			s.fail(ctx, err)
			return nil, "history inputs unavailable"
		}
		s.manifests[run.Start.Inputs] = manifest
	}
	if !sameInputs(manifest, previous.InputFiles, s.manifest, spec.InputFiles) {
		return nil, "check inputs changed"
	}
	if err := s.store.loadOutput(&previous); err != nil {
		s.fail(ctx, err)
		return nil, "history output unavailable"
	}
	return &previous, "matching successful run inputs, command, scope and environment"
}

func historyRecord(kind, checkout, root, inputs string, result CheckResult, consistent bool) historyEvent {
	result.Stdout, result.Stderr = "", ""
	return historyEvent{SchemaVersion: 2, Checkout: checkout, Type: kind, At: time.Now().UTC(), Run: result.RunID, Component: root, Inputs: inputs, Check: result, Consistent: consistent}
}

func (s *historySession) started(ctx context.Context, root string, spec CheckSpec) string {
	id := rand.Text()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store != nil && !s.failed {
		e := historyRecord("started", s.manifest.Checkout, root, s.inputID, CheckResult{CheckSpec: spec, RunID: id}, false)
		if err := s.store.append(ctx, []historyEvent{e}); err != nil {
			s.fail(ctx, err)
		} else if err := s.state.apply(e, 0); err != nil {
			s.fail(ctx, err)
		}
	}
	return id
}

func (s *historySession) finish(ctx context.Context, report *ValidationReport) {
	if s.store == nil {
		report.Diagnostics = append(report.Diagnostics, s.diagnostics...)
		return
	}
	if !s.failed {
		events := []historyEvent{}
		for _, group := range report.Components {
			for _, result := range group.Checks {
				if result.RunID == "" {
					continue
				}
				kind := "reused"
				if !result.Reused {
					kind = "finished"
					if err := s.store.saveOutput(result); err != nil {
						s.fail(ctx, err)
						break
					}
				}
				events = append(events, historyRecord(kind, report.Checkout, group.Component.Root, s.inputID, result, report.InputsConsistent))
			}
			if s.failed {
				break
			}
		}
		if !s.failed {
			if err := s.store.append(ctx, events); err != nil {
				s.fail(ctx, err)
			} else {
				for _, e := range events {
					if err := s.state.apply(e, 0); err != nil {
						s.fail(ctx, err)
						break
					}
				}
			}
		}
	}
	if !s.failed {
		state, err := s.store.read(ctx)
		if err != nil {
			s.fail(ctx, err)
		} else {
			report.History = &state.Summary
		}
	}
	report.Diagnostics = append(report.Diagnostics, s.diagnostics...)
}

// Retention removes only a prefix closed over Run references. A recent reuse or
// an unfinished run pins its start; references in the retained suffix also pin
// their starts. This preserves replay semantics instead of leaving orphan events.
func (h *historyStore) maintain(ctx context.Context, days int) (*historyState, error) {
	var state *historyState
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
				if !validHistoryID(id) {
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
