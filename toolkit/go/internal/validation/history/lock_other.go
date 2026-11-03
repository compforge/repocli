//go:build !linux && !darwin

package history

import (
	"context"
	"errors"
)

func lock(context.Context, string) (func(), error) {
	return nil, errors.New("validation history requires Linux or macOS file locking")
}
