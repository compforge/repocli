//go:build !linux && !darwin

package validation

import (
	"context"
	"errors"
)

func lockHistory(context.Context, string) (func(), error) {
	return nil, errors.New("validation history requires Linux or macOS file locking")
}
