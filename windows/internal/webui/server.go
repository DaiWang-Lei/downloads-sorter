// Package webui 提供规则编辑页面：Windows 托盘程序没有原生设置窗口，改在浏览器里编辑。
// 只监听 127.0.0.1 的随机端口，路径里带随机令牌，并校验 Host，其他网页无法读写规则。
package webui

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"downloadsorter/internal/sorter"
)

//go:embed rules.html
var page []byte

type Server struct {
	Get      func() []sorter.Rule
	Save     func([]sorter.Rule) error
	Defaults func() []sorter.Rule

	once  sync.Once
	token string
	host  string
	url   string
	err   error
}

// URL 首次调用时启动服务
func (s *Server) URL() (string, error) {
	s.once.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			s.err = err
			return
		}
		s.token = newToken()
		s.host = ln.Addr().String()
		s.url = fmt.Sprintf("http://%s/%s/", s.host, s.token)
		go http.Serve(ln, s.handler())
	})
	return s.url, s.err
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) handler() http.Handler {
	base := "/" + s.token
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+base+"/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("GET "+base+"/api/rules", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Get()) })
	mux.HandleFunc("GET "+base+"/api/defaults", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Defaults()) })
	mux.HandleFunc("PUT "+base+"/api/rules", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeJSON(w, 415, map[string]string{"error": "需要 JSON"})
			return
		}
		var rules []sorter.Rule
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&rules); err != nil {
			writeJSON(w, 400, map[string]string{"error": "格式不对：" + err.Error()})
			return
		}
		for i, rule := range rules {
			d, ok := sorter.SafeDest(rule.Dest)
			if !ok {
				writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("第 %d 条「%s」的目标文件夹不合法", i+1, rule.Name)})
				return
			}
			rules[i].Dest = d
		}
		if err := s.Save(rules); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, rules)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.host { // 防 DNS 重绑定
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
