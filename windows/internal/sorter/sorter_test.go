package sorter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setup(t *testing.T) string {
	root := t.TempDir()
	for _, d := range []string{"10_个人档案", "60_图片", "课程", "资料包", "Some.app/Contents"} {
		must(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	for _, f := range []string{"发票_20261014.pdf", "invoice-123.PDF", "支付宝交易明细(20261001).pdf", "张三简历.pdf", "Foo-1.0-arm64.dmg",
		"WeChatSetup.exe", "Qoder-Installer-win64.zip", "random.zip", "book.EPUB", "photo.HEIC", "60_图片/photo.HEIC",
		"docker-compose.yml", "report.pdf", "noext", "big.mp4.crdownload", "电影.mkv.xltd", "my 'file' (1).png", ".DS_Store",
		"desktop.ini", "~$合同.docx", "课程/a.mp4.baiduyun.p.downloading", "资料包/a.txt", "张三-61-20261021.pdf", "演示.mp4"} {
		must(t, os.WriteFile(filepath.Join(root, f), nil, 0o644))
	}
	return root
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fakeSource(p string) string {
	if filepath.Base(p) == "张三-61-20261021.pdf" {
		return "https://bwfp.baiwang.com/invoiceInfo/download?x=1"
	}
	return ""
}

func TestFreshFilesAreSkipped(t *testing.T) {
	root := setup(t)
	res, err := (&Sorter{Root: root, Rules: DefaultRules(), Settle: 2 * time.Minute, DirSettle: time.Hour, SourceOf: fakeSource}).Run()
	must(t, err)
	if len(res.Moved) != 0 {
		t.Fatalf("刚到达的文件不应移动，实际移动了 %v", res.Moved)
	}
}

func TestSortsSettledFiles(t *testing.T) {
	root := setup(t)
	rules := append(DefaultRules(),
		Rule{Name: "视频", Extensions: []string{"mp4"}, Dest: "视频"},
		Rule{Name: "越界", Keywords: []string{"report"}, Dest: "../evil"},
		Rule{Name: "盘符", Keywords: []string{"noext"}, Dest: `C:\Windows`})
	s := &Sorter{Root: root, Rules: rules, SourceOf: fakeSource}
	res, err := s.Run()
	must(t, err)
	for _, m := range res.Moved {
		t.Logf("%s → %s", m.Name, m.Target)
	}

	expect := map[string]string{
		"30_财务/发票/发票_20261014.pdf":             "文件名含发票",
		"30_财务/发票/invoice-123.PDF":              "invoice 不区分大小写",
		"30_财务/发票/张三-61-20261021.pdf":          "按下载来源 baiwang 识别发票",
		"30_财务/账单流水/支付宝交易明细(20261001).pdf":     "账单",
		"10_个人档案/简历/张三简历.pdf":                   "简历",
		"90_安装包/Foo-1.0-arm64.dmg":               "dmg",
		"90_安装包/WeChatSetup.exe":                 "exe",
		"90_安装包/Qoder-Installer-win64.zip":       "安装包 zip",
		"90_安装包/Some.app":                        ".app 文件夹按安装包处理",
		"00_收件箱/random.zip":                      "普通 zip 进收件箱",
		"40_学习/电子书/book.EPUB":                    "扩展名不区分大小写",
		"60_图片/photo (2).HEIC":                    "重名自动加 (2)",
		"60_图片/photo.HEIC":                        "原有同名文件不被覆盖",
		"60_图片/my 'file' (1).png":                 "特殊字符文件名",
		"50_代码与配置/docker-compose.yml":             "配置文件",
		"00_收件箱/report.pdf":                      "越界目标 ../evil 被拒绝",
		"00_收件箱/noext":                           `盘符目标 C:\Windows 被拒绝`,
		"00_收件箱/资料包":                             "已完成的文件夹进收件箱",
		"视频/演示.mp4":                               "自定义规则和目标目录",
		"big.mp4.crdownload":                     "Chrome/Edge 未下载完的不动",
		"电影.mkv.xltd":                            "迅雷未下载完的不动",
		"课程/a.mp4.baiduyun.p.downloading":        "含未完成下载的文件夹不动",
		".DS_Store":                              "隐藏文件不动",
		"desktop.ini":                            "desktop.ini 不动",
		"~$合同.docx":                              "Office 锁文件不动",
	}
	for path, why := range expect {
		if !exists(filepath.Join(root, filepath.FromSlash(path))) {
			t.Errorf("%s：缺少 %s", why, path)
		}
	}
	if exists(filepath.Join(filepath.Dir(root), "evil")) {
		t.Error("写到了「下载」外面")
	}
	if len(res.Failures) > 0 {
		t.Errorf("不应有失败项：%v", res.Failures)
	}

	again, err := s.Run()
	must(t, err)
	if len(again.Moved) != 0 {
		t.Errorf("再跑一次不应再移动（自定义目录「视频」要被识别为分类目录），实际 %v", again.Moved)
	}
}

func TestRulesJSONCompatibleWithMac(t *testing.T) {
	// Mac 版 rules.json 带 id 字段，Windows 版忽略即可
	data := `[{"id":"1F0B6C9E-0000-0000-0000-000000000000","name":"测试","keywords":[],"extensions":["mp3"],"sources":[],"dest":"70_音频"}]`
	var rules []Rule
	must(t, json.Unmarshal([]byte(data), &rules))
	if len(rules) != 1 || rules[0].Dest != "70_音频" || !rules[0].Matches("a.mp3", "mp3", "") {
		t.Fatalf("解析结果不对：%+v", rules)
	}
}

func TestSafeDest(t *testing.T) {
	ok := map[string]string{"60_图片": "60_图片", ` 30_财务\发票 `: "30_财务/发票", "/a//b/": "a/b"}
	for in, want := range ok {
		if got, valid := SafeDest(in); !valid || got != want {
			t.Errorf("SafeDest(%q) = %q,%v，期望 %q", in, got, valid, want)
		}
	}
	for _, bad := range []string{"", "..", "a/../b", `C:\x`, "a:b", "con?", "trailing.", "  "} {
		if got, valid := SafeDest(bad); valid {
			t.Errorf("SafeDest(%q) 应被拒绝，得到 %q", bad, got)
		}
	}
}

func TestExcludeSelf(t *testing.T) {
	for _, self := range []string{"下载整理-Windows", "下载整理.exe"} {
		root := t.TempDir()
		must(t, os.MkdirAll(filepath.Join(root, "下载整理-Windows"), 0o755))
		must(t, os.WriteFile(filepath.Join(root, "下载整理-Windows", "下载整理.exe"), nil, 0o644))
		must(t, os.WriteFile(filepath.Join(root, "下载整理.exe"), nil, 0o644))
		res, err := (&Sorter{Root: root, Rules: DefaultRules(), Exclude: self}).Run()
		must(t, err)
		if !exists(filepath.Join(root, self)) || len(res.Moved) != 1 {
			t.Errorf("程序自身所在的 %s 不应被移走，另一个应被移走；实际移动 %v", self, res.Moved)
		}
	}
}
