#!/bin/zsh
# ./build.sh        编译出 build/下载整理.app（通用二进制，Apple 芯片和 Intel 都能运行）
# ./build.sh test   运行整理逻辑的测试
set -e
cd ${0:A:h}
FLAGS=(-swift-version 5)

if [[ $1 == test ]]; then
  mkdir -p build
  swiftc $FLAGS Sources/Sorter.swift Tests/main.swift -o build/sorter-tests
  exec build/sorter-tests
fi

APP=build/下载整理.app
rm -rf $APP build/arch
mkdir -p $APP/Contents/MacOS $APP/Contents/Resources build/arch
for arch in arm64 x86_64; do
  swiftc $FLAGS -O -parse-as-library -target $arch-apple-macos14.0 Sources/*.swift -o build/arch/DownloadsSorter-$arch
done
lipo -create build/arch/DownloadsSorter-* -output $APP/Contents/MacOS/DownloadsSorter
cp Info.plist $APP/Contents/Info.plist
[[ -f AppIcon.icns ]] && cp AppIcon.icns $APP/Contents/Resources/
codesign --force --sign - $APP
echo "✅ $APP"
