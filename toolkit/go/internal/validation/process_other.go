//go:build !darwin && !linux

package validation

import (
	"errors"
	"os/exec"
)

func validationProcessGroup(cmd *exec.Cmd) (func(), error) {
	return nil, errors.New("Make validation currently requires macOS or Linux process groups")
}
