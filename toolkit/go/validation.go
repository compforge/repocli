package repocli

import (
	"context"

	"github.com/compforge/repocli/toolkit/go/internal/validation"
)

// ValidationRequest selects Makefile checks and their Component/file scope.
type ValidationRequest = validation.ValidationRequest

// CheckSpec freezes one Component/check invocation, its scope and execution inputs.
type CheckSpec = validation.CheckSpec

// CheckInputDeclaration declares complete check inputs and dependency receipts.
type CheckInputDeclaration = validation.CheckInputDeclaration

// ComponentValidationPlan groups frozen checks by their execution Component.
type ComponentValidationPlan = validation.ComponentValidationPlan

// ValidationPlan is a live, content-bound plan; its JSON is not an executable artifact.
type ValidationPlan = validation.ValidationPlan

// CheckResult records command outcome independently of round-level consistency.
type CheckResult = validation.CheckResult

// ComponentValidationResult groups check outcomes by execution Component.
type ComponentValidationResult = validation.ComponentValidationResult

// ValidationReport records execution and input consistency, not merge permission.
type ValidationReport = validation.ValidationReport

// PlanValidation reads the checkout and freezes Makefile checks without executing them.
func PlanValidation(ctx context.Context, req ValidationRequest) (ValidationPlan, error) {
	return validation.PlanValidation(ctx, req)
}

// ExecuteValidation runs a live frozen plan and verifies its input consistency.
func ExecuteValidation(ctx context.Context, plan ValidationPlan) (ValidationReport, error) {
	return validation.ExecuteValidation(ctx, plan)
}

// Validate completes optional formatting before planning and executing lint/test.
func Validate(ctx context.Context, req ValidationRequest) (ValidationReport, error) {
	return validation.Validate(ctx, req)
}

// Format runs formatting without granting lint/test evidence.
func Format(ctx context.Context, req ValidationRequest) (ValidationReport, error) {
	return validation.Format(ctx, req)
}

// Lint plans and executes only lint checks.
func Lint(ctx context.Context, req ValidationRequest) (ValidationReport, error) {
	return validation.Lint(ctx, req)
}

// Test plans and executes only test checks.
func Test(ctx context.Context, req ValidationRequest) (ValidationReport, error) {
	return validation.Test(ctx, req)
}
