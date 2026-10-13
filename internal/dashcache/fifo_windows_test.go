//go:build windows

package dashcache

import "errors"

func mkfifo(string) error { return errors.New("no FIFOs on Windows") }
