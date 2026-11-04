package stats

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/compforge/go-stdx/timeline"
)

type commandRun struct {
	started  bool
	finished bool
	SlowRun
}

type logSummary struct {
	runs              map[string]*commandRun
	analysisDurations []float64
	stages            map[string][]float64
}

func (r *Report) readLogs(ctx context.Context, top int) {
	dir := filepath.Join(r.Directory, "logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, fmt.Sprintf("logs %s: %v", dir, err))
		return
	}
	summary := logSummary{runs: map[string]*commandRun{}, stages: map[string][]float64{}}
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		var visit func([]byte) error
		switch {
		case datedLog(name, "", ".log"):
			visit = func(line []byte) error { return summary.commandLine(&r.Commands, string(line)) }
		case datedLog(name, "diff-", ".jsonl"):
			visit = func(line []byte) error { return summary.analysisLine(&r.Analysis, line) }
		default:
			continue
		}
		if err := scanLines(ctx, filepath.Join(dir, name), visit); err != nil {
			r.Diagnostics = append(r.Diagnostics, fmt.Sprintf("logs %s: %v", name, err))
		}
	}
	summary.finish(r, top)
}

func datedLog(name, prefix, suffix string) bool {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	_, err := time.Parse("2006-01-02", strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix))
	return err == nil
}

// Bound a damaged/oversized line while preserving other files and earlier valid records.
func scanLines(ctx context.Context, path string, visit func([]byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line, invalid := 0, 0
	var first error
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line++
		if err := visit(scanner.Bytes()); err != nil {
			invalid++
			if first == nil {
				first = fmt.Errorf("line %d: %w", line, err)
			}
		}
	}
	if invalid > 0 {
		first = fmt.Errorf("%d invalid records (%w)", invalid, first)
	}
	return errors.Join(first, scanner.Err())
}

// slog TextHandler quotes values with Go string escaping, not shell quoting.
var logField = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=("(?:\\.|[^"\\])*"|[^\s"]+)`)

func commandFields(line string) (map[string]string, error) {
	line, ok := strings.CutPrefix(line, "[repocli] ")
	if !ok {
		return nil, errors.New("missing repocli log prefix")
	}
	fields := map[string]string{}
	end := 0
	for _, match := range logField.FindAllStringSubmatchIndex(line, -1) {
		if strings.TrimSpace(line[end:match[0]]) != "" {
			return nil, errors.New("invalid log field")
		}
		key, value := line[match[2]:match[3]], line[match[4]:match[5]]
		if strings.HasPrefix(value, `"`) {
			var err error
			value, err = strconv.Unquote(value)
			if err != nil {
				return nil, errors.New("invalid quoted log field")
			}
		}
		fields[key] = value
		end = match[1]
	}
	if strings.TrimSpace(line[end:]) != "" || fields["msg"] == "" {
		return nil, errors.New("invalid log record")
	}
	return fields, nil
}

func (s *logSummary) commandLine(summary *Commands, line string) error {
	f, err := commandFields(line)
	if err != nil {
		return err
	}
	msg := f["msg"]
	if msg != "command.started" && msg != "command.finished" && msg != "command.failed" {
		return nil
	}
	at, err := time.Parse(time.RFC3339Nano, f["time"])
	if err != nil || f["run_id"] == "" {
		return errors.New("missing command time or run ID")
	}
	run := s.runs[f["run_id"]]
	if run == nil {
		run = &commandRun{}
	}
	if msg == "command.started" {
		if run.started {
			return errors.New("duplicate command start")
		}
		run.started, run.CWD = true, f["cwd"]
	} else {
		if run.finished {
			return errors.New("duplicate command completion")
		}
		ms, err := strconv.ParseInt(f["elapsed_ms"], 10, 64)
		code, codeErr := strconv.Atoi(f["exit_code"])
		if err != nil || codeErr != nil || ms < 0 || f["command"] == "" {
			return errors.New("invalid command outcome or duration")
		}
		if (msg == "command.finished") != (code == 0) {
			return errors.New("inconsistent command outcome")
		}
		run.finished = true
		run.RunID, run.Command, run.Time, run.Seconds, run.ExitCode = f["run_id"], f["command"], at, float64(ms)/1000, code
	}
	s.runs[f["run_id"]] = run
	includeTime(&summary.From, &summary.To, at)
	return nil
}

func (s *logSummary) analysisLine(summary *Analysis, line []byte) error {
	var record struct {
		SchemaVersion int               `json:"schemaVersion"`
		Time          time.Time         `json:"time"`
		RunID         string            `json:"runId"`
		Status        string            `json:"status"`
		Timeline      timeline.Snapshot `json:"timeline"`
	}
	if err := json.Unmarshal(line, &record); err != nil {
		return fmt.Errorf("invalid diff JSON: %w", err)
	}
	if record.SchemaVersion != 5 {
		return fmt.Errorf("unsupported diff schema %d", record.SchemaVersion)
	}
	if record.Time.IsZero() || record.RunID == "" {
		return errors.New("missing diff time or run ID")
	}
	switch record.Status {
	case "completed", "failed", "canceled", "deadline_exceeded":
	default:
		return fmt.Errorf("unknown diff status %q", record.Status)
	}
	summary.Statuses[record.Status]++
	includeTime(&summary.From, &summary.To, record.Time)
	snapshot := record.Timeline
	if !snapshot.StartedAt.IsZero() && !snapshot.FinishedAt.IsZero() && snapshot.Duration() >= 0 {
		s.analysisDurations = append(s.analysisDurations, snapshot.Duration().Seconds())
	} else {
		summary.Untimed++
	}
	for _, stage := range snapshot.Stages {
		if stage.Name == "" || stage.StartedAt.IsZero() || stage.FinishedAt.IsZero() || stage.Duration(snapshot.CapturedAt) < 0 {
			continue
		}
		s.stages[stage.Name] = append(s.stages[stage.Name], stage.Duration(snapshot.CapturedAt).Seconds())
	}
	return nil
}

func (s *logSummary) finish(r *Report, top int) {
	commands := map[string]*Command{}
	timings := map[string][]float64{}
	for _, run := range s.runs {
		if !run.finished {
			r.Commands.Unfinished++
			continue
		}
		c := commands[run.Command]
		if c == nil {
			c = &Command{Name: run.Command}
			commands[run.Command] = c
		}
		if run.ExitCode == 0 {
			c.Succeeded++
		} else {
			c.Failed++
		}
		timings[c.Name] = append(timings[c.Name], run.Seconds)
		r.Commands.Slowest = append(r.Commands.Slowest, run.SlowRun)
	}
	for _, name := range sortedKeys(commands) {
		c := commands[name]
		c.Duration = durations(timings[name])
		r.Commands.ByCommand = append(r.Commands.ByCommand, *c)
	}
	slices.SortFunc(r.Commands.Slowest, func(a, b SlowRun) int {
		if order := cmp.Compare(b.Seconds, a.Seconds); order != 0 {
			return order
		}
		return strings.Compare(a.RunID, b.RunID)
	})
	if len(r.Commands.Slowest) > top {
		r.Commands.Slowest = r.Commands.Slowest[:top]
	}
	r.Analysis.Duration = durations(s.analysisDurations)
	for _, name := range sortedKeys(s.stages) {
		r.Analysis.Stages = append(r.Analysis.Stages, Stage{Name: name, Duration: durations(s.stages[name])})
	}
}
