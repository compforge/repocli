//go:build !linux && !darwin

package validation

import "os"

func depotDirectoryIdentity(os.FileInfo) string { return "" }
