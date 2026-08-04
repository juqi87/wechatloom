//go:build windows

package publisher

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func validateUserConfigPermissions(path string, _ os.FileInfo) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("USER_CONFIG_PERMISSIONS: inspect Windows ACL: %w", err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return fmt.Errorf("USER_CONFIG_PERMISSIONS: inspect Windows owner: %w", err)
	}
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("USER_CONFIG_PERMISSIONS: inspect current Windows user: %w", err)
	}
	localSystem, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("USER_CONFIG_PERMISSIONS: resolve LocalSystem SID: %w", err)
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("USER_CONFIG_PERMISSIONS: resolve Administrators SID: %w", err)
	}
	allowed := []*windows.SID{tokenUser.User.Sid, localSystem, administrators}
	if !sidAllowed(owner, allowed) {
		return errors.New("USER_CONFIG_PERMISSIONS: Windows config must be owned by the current user, SYSTEM, or Administrators")
	}

	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return errors.New("USER_CONFIG_PERMISSIONS: Windows config requires a private DACL")
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return fmt.Errorf("USER_CONFIG_PERMISSIONS: inspect Windows ACL entry: %w", err)
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !grantsFileContentRead(ace.Mask) {
			continue
		}
		principal := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sidAllowed(principal, allowed) {
			return errors.New("USER_CONFIG_PERMISSIONS: Windows config grants read access outside the current user, SYSTEM, or Administrators")
		}
	}
	return nil
}

func grantsFileContentRead(mask windows.ACCESS_MASK) bool {
	return mask&(windows.FILE_READ_DATA|windows.GENERIC_READ|windows.GENERIC_ALL|windows.MAXIMUM_ALLOWED) != 0
}

func sidAllowed(candidate *windows.SID, allowed []*windows.SID) bool {
	for _, trusted := range allowed {
		if candidate != nil && trusted != nil && candidate.Equals(trusted) {
			return true
		}
	}
	return false
}
