// Package sorter 扫描「下载」顶层，把已经静止下来的新文件按规则移到分类目录。只移动，不删除。
// 规则与 Mac 版（Sources/Sorter.swift）保持一致，rules.json 格式互通。
package sorter

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const Fallback = "00_收件箱"

// Rule 填了的条件都要满足；每个条件里有多个值时满足其一即可。
type Rule struct {
	Name       string   `json:"name"`
	Keywords   []string `json:"keywords,omitempty"`   // 文件名包含（不区分大小写）
	Extensions []string `json:"extensions,omitempty"` // 扩展名
	Sources    []string `json:"sources,omitempty"`    // 下载来源网址包含
	Dest       string   `json:"dest"`                 // 相对「下载」的目标目录
}

func (r Rule) Matches(name, ext, source string) bool {
	kw, ex, sr := clean(r.Keywords, " "), clean(r.Extensions, ". "), clean(r.Sources, " ")
	if len(kw) == 0 && len(ex) == 0 && len(sr) == 0 {
		return false
	}
	name, source = strings.ToLower(name), strings.ToLower(source)
	if len(kw) > 0 && !anyOf(kw, func(k string) bool { return strings.Contains(name, k) }) {
		return false
	}
	if len(ex) > 0 && !anyOf(ex, func(e string) bool { return e == ext }) {
		return false
	}
	if len(sr) > 0 && !anyOf(sr, func(s string) bool { return strings.Contains(source, s) }) {
		return false
	}
	return true
}

func clean(list []string, cut string) []string {
	var out []string
	for _, s := range list {
		if s = strings.ToLower(strings.Trim(s, cut)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func anyOf(list []string, f func(string) bool) bool {
	for _, s := range list {
		if f(s) {
			return true
		}
	}
	return false
}

func DefaultRules() []Rule {
	return []Rule{
		{Name: "发票", Keywords: []string{"发票", "invoice"}, Dest: "30_财务/发票"},
		{Name: "发票平台下载", Sources: []string{"baiwang", "fapiao", "chinatax", "nuonuo"}, Dest: "30_财务/发票"},
		{Name: "账单流水", Keywords: []string{"交易明细", "账单", "流水", "对账单", "statement"}, Dest: "30_财务/账单流水"},
		{Name: "简历", Keywords: []string{"简历", "resume"}, Dest: "10_个人档案/简历"},
		{Name: "安装包", Extensions: []string{"exe", "msi", "msix", "msixbundle", "appx", "dmg", "pkg", "app", "vsix"}, Dest: "90_安装包"},
		{Name: "安装包 zip", Keywords: []string{"installer", "setup", "windows", "win64", "win32", "x64", "x86", "arm64", "portable", "macos", "darwin"},
			Extensions: []string{"zip", "7z", "rar"}, Dest: "90_安装包"},
		{Name: "电子书", Extensions: []string{"epub", "mobi", "azw3"}, Dest: "40_学习/电子书"},
		{Name: "图片", Extensions: []string{"jpg", "jpeg", "png", "heic", "gif", "webp", "avif", "bmp", "tiff", "svg"}, Dest: "60_图片"},
		{Name: "代码与配置", Extensions: []string{"yml", "yaml", "conf", "toml", "ini", "json", "sh", "ps1", "bat", "py", "sql", "diff", "patch"},
			Dest: "50_代码与配置"},
	}
}

type Record struct {
	Time   time.Time `json:"time"`
	Name   string    `json:"name"`   // 原文件名
	Target string    `json:"target"` // 移动后相对「下载」的路径，用 / 分隔
}

type Result struct {
	Moved    []Record
	Failures []string
	Busy     []string // 被其他程序占用（Windows 上正在写入/打开的文件），下一轮再试
}

type Sorter struct {
	Root      string
	Rules     []Rule
	Settle    time.Duration            // 文件到达后静止多久才处理，避免搬走正在写入的文件
	DirSettle time.Duration            // 文件夹（如网盘下载）要静止更久
	SourceOf  func(path string) string // 下载来源网址，Windows 上读 Zone.Identifier
	Exclude   string                   // 顶层不处理的条目（程序自己放在「下载」里时，别把自己移走）
}

var partialSuffixes = []string{".crdownload", ".download", ".part", ".partial", ".downloading", ".tmp", ".opdownload",
	".xltd", ".td", ".td.cfg", ".!ut", ".bc!", ".aria2"}

func (s *Sorter) Run() (Result, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return Result{}, err
	}
	now := time.Now()
	roots := map[string]bool{strings.ToLower(Fallback): true}
	for _, r := range s.Rules {
		if d, ok := SafeDest(r.Dest); ok {
			roots[strings.ToLower(strings.Split(d, "/")[0])] = true
		}
	}

	var res Result
	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(s.Root, name)
		if skipName(name) || IsManaged(name) || roots[strings.ToLower(name)] || IsPartial(name) || isHidden(p) ||
			(s.Exclude != "" && strings.EqualFold(name, s.Exclude)) {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		isDir := e.IsDir()
		var dest string
		if isDir && ext != "app" {
			if containsPartial(p) || now.Sub(newestChange(p)) < s.DirSettle {
				continue
			}
			dest = Fallback
		} else {
			changed := changeTime(p)
			if isDir {
				changed = newestChange(p)
			}
			if now.Sub(changed) < s.Settle {
				continue
			}
			source := ""
			if s.SourceOf != nil {
				source = s.SourceOf(p)
			}
			dest = s.Destination(name, ext, source)
		}

		dir := filepath.Join(s.Root, filepath.FromSlash(dest))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			res.Failures = append(res.Failures, fmt.Sprintf("%s 移动失败：%v", name, err))
			continue
		}
		target := uniqueTarget(dir, name, isDir)
		if err := os.Rename(p, target); err != nil {
			if isBusy(err) {
				res.Busy = append(res.Busy, name)
			} else {
				res.Failures = append(res.Failures, fmt.Sprintf("%s 移动失败：%v", name, err))
			}
			continue
		}
		res.Moved = append(res.Moved, Record{Time: now, Name: name, Target: dest + "/" + filepath.Base(target)})
	}
	return res, nil
}

func (s *Sorter) Destination(name, ext, source string) string {
	for _, r := range s.Rules {
		if r.Matches(name, ext, source) {
			if d, ok := SafeDest(r.Dest); ok {
				return d
			}
		}
	}
	return Fallback
}

// SafeDest 只允许「下载」内部的相对路径：拒绝 ..、盘符、绝对路径和 Windows 文件名非法字符
func SafeDest(s string) (string, bool) {
	var parts []string
	for _, p := range strings.Split(strings.ReplaceAll(s, `\`, "/"), "/") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "." || p == ".." || strings.ContainsAny(p, `:*?"<>|`) || strings.HasSuffix(p, ".") {
			return "", false
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "/"), true
}

// IsManaged 已整理好的分类目录，如 00_收件箱、60_图片
func IsManaged(name string) bool {
	return len(name) >= 3 && isDigit(name[0]) && isDigit(name[1]) && name[2] == '_'
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func IsPartial(name string) bool {
	lower := strings.ToLower(name)
	for _, suf := range partialSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return false
}

// 隐藏文件、系统文件和 Office 打开文档时生成的 ~$ 锁文件
func skipName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") || lower == "desktop.ini" || lower == "thumbs.db"
}

var errFound = errors.New("found")

func containsPartial(dir string) bool {
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && IsPartial(d.Name()) {
			return errFound
		}
		return nil
	})
	return errors.Is(err, errFound)
}

func newestChange(dir string) time.Time {
	newest := changeTime(dir)
	_ = filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err == nil {
			if t := changeTime(p); t.After(newest) {
				newest = t
			}
		}
		return nil
	})
	return newest
}

func uniqueTarget(dir, name string, isDir bool) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	target := filepath.Join(dir, name)
	for i := 2; exists(target); i++ {
		if !isDir && ext != "" && stem != "" {
			target = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		} else {
			target = filepath.Join(dir, fmt.Sprintf("%s (%d)", name, i))
		}
	}
	return target
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
