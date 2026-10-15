#!/bin/zsh
# 跑测试并打包两个平台的发布产物到 dist/（GitHub Release 附件名不支持中文，所以用英文名）：
#   dist/DownloadsSorter-macOS.zip    （下载整理.app，通用二进制）
#   dist/DownloadsSorter-Windows.zip  （下载整理.exe + 使用说明）
set -e
cd ${0:A:h}
./build.sh test
./build.sh
windows/build-windows.sh
rm -rf dist && mkdir -p dist
ditto -c -k --keepParent build/下载整理.app dist/DownloadsSorter-macOS.zip
cp windows/dist/下载整理-Windows.zip dist/DownloadsSorter-Windows.zip
cd dist && shasum -a 256 *.zip | tee SHA256SUMS.txt
