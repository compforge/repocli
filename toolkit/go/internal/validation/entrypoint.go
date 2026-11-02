package validation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/compforge/repokit/toolkit/go/internal/project"
)

func validationMakeContract(ctx context.Context, checkout, component string) (makeContract, error) {
	directory := filepath.Join(checkout, component)
	real, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return makeContract{}, err
	}
	if real != directory {
		return makeContract{}, fmt.Errorf("Component %q crosses a symlink", component)
	}
	makefile := filepath.Join(directory, "Makefile")
	info, err := os.Lstat(makefile)
	if err == nil && !info.Mode().IsRegular() {
		return makeContract{}, fmt.Errorf("Component %q Makefile must be a regular file", component)
	}
	text, err := os.ReadFile(makefile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return makeContract{}, err
	}
	return readMakeContract(ctx, text), nil
}

// +spec=Each execution Component/check runs once; full requests dominate focused requests and focused files are unioned
// +why=Makefile recipes are opaque; parent/child coverage is never inferred from nested make calls
// +link=component://docs/validation.md
func appendValidationCheck(plan *ValidationPlan, executor project.Binding, spec CheckSpec) {
	for i := range plan.Components {
		group := &plan.Components[i]
		if group.Component.Root != executor.Root {
			continue
		}
		for j := range group.Checks {
			previous := &group.Checks[j]
			if previous.Check != spec.Check {
				continue
			}
			sources := append(slices.Clone(previous.RequestedComponents), spec.RequestedComponents...)
			sort.Strings(sources)
			sources = slices.Compact(sources)
			switch {
			case previous.Scope == "skipped":
				*previous = spec
			case spec.Scope == "skipped":
			case previous.Scope == "full":
			case spec.Scope == "full":
				*previous = spec
			case previous.Scope == "focused" && spec.Scope == "focused":
				previous.Files = append(slices.Clone(previous.Files), spec.Files...)
				sort.Strings(previous.Files)
				previous.Files = slices.Compact(previous.Files)
				idx, variable := 4, "LINT_FILES"
				if previous.Check == "test" {
					idx, variable = 5, "TEST_FILES"
				}
				previous.Command[idx] = variable + "=" + strings.Join(previous.Files, " ")
			}
			previous.RequestedComponents = sources
			if len(sources) > 1 && !strings.HasPrefix(previous.Reason, "merged Component requests; ") {
				previous.Reason = "merged Component requests; " + previous.Reason
			}
			return
		}
		group.Checks = append(group.Checks, spec)
		return
	}
	plan.Components = append(plan.Components, ComponentValidationPlan{Component: executor, Checks: []CheckSpec{spec}})
}
