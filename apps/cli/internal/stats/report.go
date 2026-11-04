// Package stats summarizes repocli's retained local records, not repository health.
package stats

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/compforge/repokit/toolkit/go"
)

type Report struct {
	Directory   string     `json:"directory"`
	CollectedAt time.Time  `json:"collectedAt"`
	Complete    bool       `json:"complete"`
	Storage     []Storage  `json:"storage"`
	Validation  Validation `json:"validation"`
	Commands    Commands   `json:"commands"`
	Analysis    Analysis   `json:"analysis"`
	Diagnostics []string   `json:"diagnostics"`
}

type Storage struct {
	Name             string    `json:"name"`
	Files            int       `json:"files"`
	Bytes            int64     `json:"bytes"`
	Symlinks         int       `json:"symlinks"`
	OldestModifiedAt time.Time `json:"oldestModifiedAt,omitzero"`
	NewestModifiedAt time.Time `json:"newestModifiedAt,omitzero"`
}

type Validation struct {
	Repositories []repokit.ValidationHistory `json:"repositories"`
	Total        repokit.HistoryCounts       `json:"total"`
	PassRate     float64                     `json:"passRate"`
	ReuseRate    float64                     `json:"reuseRate"`
}

type Duration struct {
	Samples      int     `json:"samples"`
	TotalSeconds float64 `json:"totalSeconds"`
	P50Seconds   float64 `json:"p50Seconds"`
	P95Seconds   float64 `json:"p95Seconds"`
	MaxSeconds   float64 `json:"maxSeconds"`
}

type Command struct {
	Name      string   `json:"name"`
	Succeeded int      `json:"succeeded"`
	Failed    int      `json:"failed"`
	Duration  Duration `json:"duration"`
}

type SlowRun struct {
	RunID    string    `json:"runId"`
	Command  string    `json:"command"`
	Time     time.Time `json:"time"`
	Seconds  float64   `json:"seconds"`
	ExitCode int       `json:"exitCode"`
	CWD      string    `json:"cwd,omitempty"`
}

type Commands struct {
	From       time.Time `json:"from,omitzero"`
	To         time.Time `json:"to,omitzero"`
	Unfinished int       `json:"unfinished"`
	ByCommand  []Command `json:"byCommand"`
	Slowest    []SlowRun `json:"slowest"`
}

type Stage struct {
	Name     string   `json:"name"`
	Duration Duration `json:"duration"`
}

type Analysis struct {
	Untimed  int            `json:"untimed"`
	From     time.Time      `json:"from,omitzero"`
	To       time.Time      `json:"to,omitzero"`
	Statuses map[string]int `json:"statuses"`
	Duration Duration       `json:"duration"`
	Stages   []Stage        `json:"stages"`
}

// Read reports best-effort observations; a damaged store never becomes a zero-count success.
// +spec=Read retained data without running checks or retention cleanup; overlapping durations remain separate
// +link=../../../../docs/stats.md
func Read(ctx context.Context, directory string, top int) (Report, error) {
	r := Report{Directory: directory, CollectedAt: time.Now(), Storage: []Storage{}, Diagnostics: []string{},
		Validation: Validation{Repositories: []repokit.ValidationHistory{}},
		Commands:   Commands{ByCommand: []Command{}, Slowest: []SlowRun{}},
		Analysis:   Analysis{Statuses: map[string]int{}, Stages: []Stage{}},
	}
	if top < 1 || top > 100 {
		return r, fmt.Errorf("top must be between 1 and 100")
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	info, err := os.Lstat(directory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return r, fmt.Errorf("read state directory %s: %w", directory, err)
	}
	if err == nil && !info.IsDir() {
		return r, fmt.Errorf("state path %s is not a regular directory", directory)
	}
	r.readStorage(ctx)
	if r.regularDirectory("repositories") {
		histories, err := repokit.ReadValidationHistories(ctx, filepath.Join(directory, "repositories"))
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, "validation: "+err.Error())
		}
		r.Validation.Repositories = histories.Repositories
		r.Diagnostics = append(r.Diagnostics, histories.Diagnostics...)
		for _, repo := range histories.Repositories {
			for _, counts := range repo.Checks {
				addCounts(&r.Validation.Total, counts)
			}
		}
		c := r.Validation.Total
		if c.Passed+c.Failed > 0 {
			r.Validation.PassRate = float64(c.Passed) / float64(c.Passed+c.Failed)
		}
		if c.Executions+c.Reuses > 0 {
			r.Validation.ReuseRate = float64(c.Reuses) / float64(c.Executions+c.Reuses)
		}
	}
	if r.regularDirectory("logs") {
		r.readLogs(ctx, top)
	}
	r.Complete = len(r.Diagnostics) == 0
	return r, ctx.Err()
}

func addCounts(total *repokit.HistoryCounts, c repokit.HistoryCounts) {
	total.Executions += c.Executions
	total.Passed += c.Passed
	total.Failed += c.Failed
	total.Unfinished += c.Unfinished
	total.Other += c.Other
	total.Reuses += c.Reuses
	total.ExecutionSeconds += c.ExecutionSeconds
	total.LookupSeconds += c.LookupSeconds
	if c.LastCheckedAt.After(total.LastCheckedAt) {
		total.LastCheckedAt = c.LastCheckedAt
	}
}

func durations(values []float64) Duration {
	d := Duration{Samples: len(values)}
	if len(values) == 0 {
		return d
	}
	slices.Sort(values)
	for _, v := range values {
		d.TotalSeconds += v
	}
	d.P50Seconds = values[int(math.Ceil(float64(len(values))*.50))-1]
	d.P95Seconds = values[int(math.Ceil(float64(len(values))*.95))-1]
	d.MaxSeconds = values[len(values)-1]
	return d
}

func includeTime(from, to *time.Time, at time.Time) {
	if from.IsZero() || at.Before(*from) {
		*from = at
	}
	if at.After(*to) {
		*to = at
	}
}
