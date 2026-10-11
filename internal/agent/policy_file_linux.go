//go:build linux

package agent

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func LoadRootPolicy(path string) (*Policy, error) {
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("stat agent policy directory: %w", err)
	}
	parentStat, ok := parent.Sys().(*unix.Stat_t)
	if !ok || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || parent.Mode().Perm()&0022 != 0 || parentStat.Uid != 0 {
		return nil, fmt.Errorf("agent policy directory must be root-owned and non-writable by group/other")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open agent policy without following symlinks: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0022 != 0 || stat.Uid != 0 {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("agent policy must be root-owned, regular, and non-writable by group/other")
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	return ParsePolicy(file)
}

func validatePolicyFile(info os.FileInfo, requireRoot bool) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("agent policy must be a regular non-writable file")
	}
	stat, ok := info.Sys().(*unix.Stat_t)
	if !ok {
		return fmt.Errorf("agent policy ownership cannot be verified")
	}
	if requireRoot && stat.Uid != 0 {
		return fmt.Errorf("agent policy must be owned by root")
	}
	return nil
}
