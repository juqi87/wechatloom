//go:build !windows

package publisher

import (
	"fmt"
	"os"
)

func validateUserConfigPermissions(path string, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("USER_CONFIG_PERMISSIONS: %s must use permissions 0600", path)
	}
	return nil
}
