//go:build linux || darwin

package validation

import (
	"fmt"
	"os"
	"syscall"
)

func depotDirectoryIdentity(info os.FileInfo) string {
	stat := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}
