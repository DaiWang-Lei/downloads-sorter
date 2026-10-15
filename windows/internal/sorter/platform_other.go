//go:build !windows

// 非 Windows 实现只用于在 Mac 上跑测试
package sorter

import (
	"os"
	"syscall"
	"time"
)

func changeTime(p string) time.Time {
	fi, err := os.Lstat(p)
	if err != nil {
		return time.Time{}
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return statCtime(st)
	}
	return fi.ModTime()
}

func isHidden(string) bool { return false }

func isBusy(error) bool { return false }

func ZoneSource(string) string { return "" }
