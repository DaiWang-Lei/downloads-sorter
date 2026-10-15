package webui

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"downloadsorter/internal/sorter"
)

func start(t *testing.T) (*Server, *[]sorter.Rule) {
	saved := sorter.DefaultRules()
	s := &Server{
		Get:      func() []sorter.Rule { return saved },
		Save:     func(r []sorter.Rule) error { saved = r; return nil },
		Defaults: sorter.DefaultRules,
	}
	if _, err := s.URL(); err != nil {
		t.Fatal(err)
	}
	return s, &saved
}

func do(t *testing.T, method, url, body string, header map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	for k, v := range header {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestPageAndRules(t *testing.T) {
	s, _ := start(t)
	if code, body := do(t, "GET", s.url, "", nil); code != 200 || !strings.Contains(body, "整理规则") {
		t.Fatalf("页面: %d", code)
	}
	if code, body := do(t, "GET", s.url+"api/rules", "", nil); code != 200 || !strings.Contains(body, "30_财务/发票") {
		t.Fatalf("读取规则: %d %s", code, body)
	}
}

func TestRequiresToken(t *testing.T) {
	s, _ := start(t)
	base := "http://" + s.host + "/"
	for _, u := range []string{base, base + "api/rules", base + "wrong/api/rules"} {
		if code, _ := do(t, "GET", u, "", nil); code != 404 {
			t.Errorf("%s 不带正确令牌应 404，实际 %d", u, code)
		}
	}
}

func TestRejectsForeignHost(t *testing.T) {
	s, _ := start(t)
	if code, _ := do(t, "GET", s.url+"api/rules", "", map[string]string{"Host": "evil.example:80"}); code != 403 {
		t.Errorf("Host 不对应 403，实际 %d", code)
	}
}

func TestSaveValidates(t *testing.T) {
	s, saved := start(t)
	json := map[string]string{"Content-Type": "application/json"}

	if code, body := do(t, "PUT", s.url+"api/rules", `[{"name":"坏","extensions":["mp4"],"dest":"../外面"}]`, json); code != 400 {
		t.Errorf("越界目标应 400，实际 %d %s", code, body)
	}
	if code, _ := do(t, "PUT", s.url+"api/rules", `[{"name":"x","dest":"a"}]`, map[string]string{"Content-Type": "text/plain"}); code != 415 {
		t.Errorf("非 JSON 应 415，实际 %d", code)
	}
	if len(*saved) != len(sorter.DefaultRules()) {
		t.Fatal("被拒绝的请求不应改动规则")
	}

	code, body := do(t, "PUT", s.url+"api/rules", `[{"name":"视频","extensions":["mp4"],"dest":" 70_视频\\电影 "}]`, json)
	if code != 200 || len(*saved) != 1 || (*saved)[0].Dest != "70_视频/电影" {
		t.Fatalf("保存: %d %s %+v", code, body, *saved)
	}
}
