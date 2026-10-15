package sorter

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// Windows 没有 ctime：取创建时间和最后写入时间中较晚的一个。
// 浏览器下载时会持续写入；从别处复制进来时创建时间是复制那一刻。
func changeTime(p string) time.Time {
	fi, err := os.Lstat(p)
	if err != nil {
		return time.Time{}
	}
	t := fi.ModTime()
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		if c := time.Unix(0, d.CreationTime.Nanoseconds()); c.After(t) {
			t = c
		}
	}
	return t
}

func isHidden(p string) bool {
	ptr, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(ptr)
	return err == nil && attrs&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM) != 0
}

// 文件正被其他程序写入或打开时 Windows 不允许移动
func isBusy(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// ZoneSource 读取浏览器写入的 Zone.Identifier 数据流（文件属性里「此文件来自其他计算机」的来源）
func ZoneSource(p string) string {
	f, err := os.Open(p + ":Zone.Identifier")
	if err != nil {
		return ""
	}
	defer f.Close()
	var urls []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		for _, key := range []string{"HostUrl=", "ReferrerUrl="} {
			if strings.HasPrefix(line, key) {
				urls = append(urls, strings.TrimPrefix(line, key))
			}
		}
	}
	return strings.Join(urls, " ")
}
