//go:build !windows

package dashcache

import "syscall"

func mkfifo(p string) error { return syscall.Mkfifo(p, 0o600) }
