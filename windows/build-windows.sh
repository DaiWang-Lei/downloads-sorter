#!/bin/zsh
# 在 Mac 上交叉编译 Windows 版：./build-windows.sh
# 产物：dist/下载整理.exe，以及附带使用说明的 dist/下载整理-Windows.zip
set -e
cd ${0:A:h}
export PATH=$HOME/sdk/go/bin:$PATH CGO_ENABLED=0
export GOPROXY=${GOPROXY:-https://goproxy.cn,direct}

go test ./internal/...
# exe 的图标、版本信息和清单（高 DPI 适配，菜单不发虚）
go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --icon assets/icon-256.png --manifest gui \
  --product-name 下载整理 --file-description 下载整理 --product-version 1.0.0 --file-version 1.0.0 --out rsrc
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w" -o dist/下载整理.exe .
python3 -I tools/make_zip.py dist/下载整理-Windows.zip dist/下载整理.exe 使用说明.txt
echo "✅ dist/下载整理.exe、dist/下载整理-Windows.zip"
