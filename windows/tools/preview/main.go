// 在 Mac 上预览规则编辑页：go run ./tools/preview，然后打开输出的网址。规则只保存在内存里。
package main

import (
	"fmt"
	"os"
	"os/signal"

	"downloadsorter/internal/sorter"
	"downloadsorter/internal/webui"
)

func main() {
	rules := sorter.DefaultRules()
	s := &webui.Server{
		Get:      func() []sorter.Rule { return rules },
		Save:     func(r []sorter.Rule) error { rules = r; return nil },
		Defaults: sorter.DefaultRules,
	}
	url, err := s.URL()
	if err != nil {
		panic(err)
	}
	fmt.Println(url)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}
