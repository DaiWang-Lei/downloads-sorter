// 下载整理 Windows 版：托盘程序，每 10 秒检查一次「下载」，把静止下来的新文件按规则归类。
package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"downloadsorter/internal/sorter"
	"downloadsorter/internal/webui"
)

//go:embed assets/tray.ico
var trayIcon []byte

const (
	appName     = "下载整理"
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
	settle      = 2 * time.Minute
	recentSlots = 10
)

type state struct {
	Enabled  bool            `json:"enabled"`
	Welcomed bool            `json:"welcomed"`
	Recent   []sorter.Record `json:"recent"`
}

type app struct {
	mu       sync.Mutex // 保护 rules、st、errText、reported
	runMu    sync.Mutex // 同一时间只跑一轮整理
	root     string
	dataDir  string
	exe      string
	rules    []sorter.Rule
	st       state
	errText  string
	reported map[string]bool // 已写进日志的失败，避免每 10 秒重复记录
	web      *webui.Server

	mEnabled, mRunNow, mInbox, mError, mStartup *systray.MenuItem
	recentItems                                 []*systray.MenuItem
}

func main() {
	name, _ := windows.UTF16PtrFromString(`Local\DownloadsSorter`)
	if _, err := windows.CreateMutex(nil, false, name); errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		msg("下载整理已经在运行了，图标在任务栏右下角（可能收在 ^ 里）。")
		return
	}
	a, err := newApp()
	if err != nil {
		msg("启动失败：" + err.Error())
		return
	}
	systray.Run(a.onReady, nil)
}

func newApp() (*app, error) {
	root, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
	if err != nil {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, "Downloads")
	}
	cfg, err := os.UserConfigDir() // %APPDATA%
	if err != nil {
		return nil, err
	}
	exe, _ := os.Executable()
	a := &app{root: root, dataDir: filepath.Join(cfg, appName), exe: exe, reported: map[string]bool{}, st: state{Enabled: true}}
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return nil, err
	}
	a.rules = a.loadRules()
	if b, err := os.ReadFile(a.path("state.json")); err == nil {
		_ = json.Unmarshal(b, &a.st)
	}
	a.web = &webui.Server{Get: a.getRules, Save: a.saveRules, Defaults: sorter.DefaultRules}
	return a, nil
}

func (a *app) path(name string) string { return filepath.Join(a.dataDir, name) }

// MARK: 托盘菜单

func (a *app) onReady() {
	systray.SetIcon(trayIcon)
	systray.SetTooltip(appName)

	a.mEnabled = systray.AddMenuItemCheckbox("自动整理", "下载完成的文件自动按规则归类", a.st.Enabled)
	a.mRunNow = systray.AddMenuItem("立即整理", "只等 10 秒，不用等满 2 分钟")
	a.mInbox = systray.AddMenuItem("打开收件箱", "")
	mRecent := systray.AddMenuItem("最近整理", "点击在资源管理器中定位")
	for i := 0; i < recentSlots; i++ {
		item := mRecent.AddSubMenuItem("", "")
		item.Hide()
		a.recentItems = append(a.recentItems, item)
		go a.onRecentClick(i, item)
	}
	a.mError = systray.AddMenuItem("", "点击打开日志")
	a.mError.Hide()
	systray.AddSeparator()
	mRules := systray.AddMenuItem("编辑规则…", "在浏览器里编辑整理规则")
	mDownloads := systray.AddMenuItem("打开「下载」文件夹", "")
	mLog := systray.AddMenuItem("打开日志", "")
	a.mStartup = systray.AddMenuItemCheckbox("开机自动启动", "", a.startupEnabled())
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "")

	a.refreshMenu()
	go a.loop()
	if !a.st.Welcomed {
		a.mu.Lock()
		a.st.Welcomed = true
		a.mu.Unlock()
		a.saveState()
		go msg("下载整理已在后台运行。\n\n图标在任务栏右下角（没看到的话点 ^ 展开），点击图标打开菜单。")
	}

	go func() {
		for {
			select {
			case <-a.mEnabled.ClickedCh:
				a.toggleEnabled()
			case <-a.mRunNow.ClickedCh:
				go a.run(10 * time.Second)
			case <-a.mInbox.ClickedCh:
				a.openDir(filepath.Join(a.root, sorter.Fallback))
			case <-a.mError.ClickedCh:
				a.openLog()
			case <-mRules.ClickedCh:
				a.openRules()
			case <-mDownloads.ClickedCh:
				shellOpen(a.root)
			case <-mLog.ClickedCh:
				a.openLog()
			case <-a.mStartup.ClickedCh:
				a.toggleStartup()
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func (a *app) refreshMenu() {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := countVisible(filepath.Join(a.root, sorter.Fallback))
	a.mInbox.SetTitle(fmt.Sprintf("打开收件箱（%d 项待整理）", n))
	tip := fmt.Sprintf("%s · 收件箱 %d 项", appName, n)
	if !a.st.Enabled {
		tip += " · 已暂停"
	}
	if a.errText != "" {
		a.mError.SetTitle("⚠ " + label(a.errText, 50))
		a.mError.Show()
		tip += " · 有错误"
	} else {
		a.mError.Hide()
	}
	systray.SetTooltip(tip)
	for i, item := range a.recentItems {
		switch {
		case i < len(a.st.Recent):
			r := a.st.Recent[i]
			item.SetTitle(label(r.Name, 36) + "  →  " + label(filepath.ToSlash(filepath.Dir(filepath.FromSlash(r.Target))), 24))
			item.Enable()
			item.Show()
		case i == 0:
			item.SetTitle("还没有整理记录")
			item.Disable()
			item.Show()
		default:
			item.Hide()
		}
	}
}

func (a *app) onRecentClick(i int, item *systray.MenuItem) {
	for range item.ClickedCh {
		a.mu.Lock()
		var rec *sorter.Record
		if i < len(a.st.Recent) {
			r := a.st.Recent[i]
			rec = &r
		}
		a.mu.Unlock()
		if rec != nil {
			a.reveal(*rec)
		}
	}
}

// MARK: 整理

func (a *app) loop() {
	tick := time.NewTicker(10 * time.Second)
	for ; ; <-tick.C {
		a.mu.Lock()
		on := a.st.Enabled
		a.mu.Unlock()
		if on {
			a.run(settle)
		} else {
			a.refreshMenu()
		}
	}
}

func (a *app) run(settle time.Duration) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	s := &sorter.Sorter{Root: a.root, Rules: a.getRules(), Settle: settle, DirSettle: time.Hour,
		SourceOf: sorter.ZoneSource, Exclude: a.selfEntry()}
	res, err := s.Run()

	var lines []string
	a.mu.Lock()
	a.errText = ""
	if err != nil {
		a.errText = "读取「下载」失败：" + err.Error()
	} else if len(res.Failures) > 0 {
		a.errText = res.Failures[0]
	}
	for _, f := range append(res.Failures, a.errText) {
		if f != "" && !a.reported[f] {
			a.reported[f] = true
			lines = append(lines, f)
		}
	}
	for i := len(res.Moved) - 1; i >= 0; i-- {
		a.st.Recent = append([]sorter.Record{res.Moved[i]}, a.st.Recent...)
	}
	if len(a.st.Recent) > 30 {
		a.st.Recent = a.st.Recent[:30]
	}
	for _, m := range res.Moved {
		lines = append(lines, m.Name+" → "+m.Target)
	}
	a.mu.Unlock()

	if len(res.Moved) > 0 {
		a.saveState()
	}
	a.log(lines)
	a.refreshMenu()
}

// 程序自己放在「下载」里时，不要把自己（或所在的文件夹）移走
func (a *app) selfEntry() string {
	rel, err := filepath.Rel(a.root, a.exe)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return strings.Split(rel, string(filepath.Separator))[0]
}

func (a *app) toggleEnabled() {
	a.mu.Lock()
	a.st.Enabled = !a.st.Enabled
	on := a.st.Enabled
	a.mu.Unlock()
	if on {
		a.mEnabled.Check()
		go a.run(settle)
	} else {
		a.mEnabled.Uncheck()
	}
	a.saveState()
	a.refreshMenu()
}

// MARK: 开机启动（写在当前用户的 Run 注册表项，不需要管理员权限）

func (a *app) startupEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(appName)
	if err != nil {
		return false
	}
	if want := `"` + a.exe + `"`; v != want { // 程序换了位置，跟着更新
		_ = k.SetStringValue(appName, want)
	}
	return true
}

func (a *app) toggleStartup() {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		a.setError("开机启动设置失败：" + err.Error())
		return
	}
	defer k.Close()
	if a.mStartup.Checked() {
		if err := k.DeleteValue(appName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			a.setError("开机启动设置失败：" + err.Error())
			return
		}
		a.mStartup.Uncheck()
		return
	}
	if a.inTempOrDownloads() {
		go msg("请先把「下载整理.exe」放到一个固定的位置（例如 D:\\Tools\\下载整理\\），从那里运行后再开启开机启动。\n\n现在它在「下载」或临时文件夹里（直接从压缩包里双击运行也会这样），以后可能被移走或清理掉。")
		return
	}
	if err := k.SetStringValue(appName, `"`+a.exe+`"`); err != nil {
		a.setError("开机启动设置失败：" + err.Error())
		return
	}
	a.mStartup.Check()
}

func (a *app) inTempOrDownloads() bool {
	exe := strings.ToLower(a.exe)
	for _, dir := range []string{a.root, os.TempDir()} {
		if d := strings.ToLower(filepath.Clean(dir)); d != "" && strings.HasPrefix(exe, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// MARK: 规则与状态

func (a *app) loadRules() []sorter.Rule {
	b, err := os.ReadFile(a.path("rules.json"))
	if err != nil {
		return sorter.DefaultRules()
	}
	var rules []sorter.Rule
	if err := json.Unmarshal(b, &rules); err != nil {
		a.log([]string{"rules.json 格式有误，已改用默认规则：" + err.Error()})
		return sorter.DefaultRules()
	}
	return rules
}

func (a *app) getRules() []sorter.Rule {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]sorter.Rule(nil), a.rules...)
}

func (a *app) saveRules(rules []sorter.Rule) error {
	b, _ := json.MarshalIndent(rules, "", "  ")
	if err := writeAtomic(a.path("rules.json"), b); err != nil {
		return err
	}
	a.mu.Lock()
	a.rules = rules
	a.mu.Unlock()
	a.log([]string{fmt.Sprintf("规则已更新（%d 条）", len(rules))})
	return nil
}

func (a *app) saveState() {
	a.mu.Lock()
	b, _ := json.MarshalIndent(a.st, "", "  ")
	a.mu.Unlock()
	_ = writeAtomic(a.path("state.json"), b)
}

func (a *app) setError(text string) {
	a.mu.Lock()
	a.errText = text
	a.mu.Unlock()
	a.log([]string{text})
	a.refreshMenu()
}

func (a *app) log(lines []string) {
	if len(lines) == 0 {
		return
	}
	p := a.path(appName + ".log")
	_, statErr := os.Stat(p)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if os.IsNotExist(statErr) {
		_, _ = f.WriteString("\uFEFF") // 让旧版记事本也按 UTF-8 打开
	}
	stamp := time.Now().Format("2006-01-02 15:04:05")
	for _, l := range lines {
		_, _ = fmt.Fprintf(f, "%s  %s\r\n", stamp, l)
	}
}

// MARK: 打开文件、文件夹、网页

func (a *app) openRules() {
	url, err := a.web.URL()
	if err != nil {
		a.setError("规则编辑页启动失败：" + err.Error())
		return
	}
	shellOpen(url)
}

func (a *app) openLog() {
	p := a.path(appName + ".log")
	if _, err := os.Stat(p); err != nil {
		a.log([]string{"日志已创建"})
	}
	shellOpen(p)
}

func (a *app) openDir(dir string) {
	_ = os.MkdirAll(dir, 0o755)
	shellOpen(dir)
}

func (a *app) reveal(r sorter.Record) {
	p := filepath.Join(a.root, filepath.FromSlash(r.Target))
	if _, err := os.Stat(p); err != nil {
		a.openDir(filepath.Dir(p))
		return
	}
	// explorer 的 /select 参数需要原样传，不能被 Go 自动加引号
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + p + `"`}
	_ = cmd.Start()
}

func shellOpen(target string) {
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(target)
	_ = windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

func msg(text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(appName)
	const mbIconInformation, mbSetForeground = 0x40, 0x10000
	_, _ = windows.MessageBox(0, t, c, mbIconInformation|mbSetForeground)
}

// MARK: 小工具

func countVisible(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if lower := strings.ToLower(e.Name()); !strings.HasPrefix(lower, ".") && lower != "desktop.ini" && lower != "thumbs.db" {
			n++
		}
	}
	return n
}

// 菜单文字：过长时截断，并转义 &（Windows 菜单会把它当成快捷键标记）
func label(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		half := (max - 1) / 2
		s = string(r[:half]) + "…" + string(r[len(r)-(max-1-half):])
	}
	return strings.ReplaceAll(s, "&", "&&")
}

func writeAtomic(p string, data []byte) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
