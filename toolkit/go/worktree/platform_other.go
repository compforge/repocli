//go:build !linux && !darwin

package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

func repositoryLease(context.Context, string) (func(), error) {
	return nil, errors.New("worktree maintenance requires POSIX file locking")
}
func gitProcessGroup(*exec.Cmd)               {}
func directoryIdentity(os.FileInfo) [2]uint64 { return [2]uint64{} }
