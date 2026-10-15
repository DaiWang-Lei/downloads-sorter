# 下载整理

自动整理「下载」文件夹的小工具，支持 macOS 和 Windows。

新文件下载完成后，按规则移到对应的分类文件夹：发票进 `30_财务/发票`、安装包进 `90_安装包`、图片进 `60_图片`……规则判断不了的放进 `00_收件箱`，留给你每周手动整理一次。**只移动，不删除任何文件。**

- **macOS**：菜单栏应用（SwiftUI），在设置窗口里编辑规则
- **Windows**：任务栏托盘程序（Go），在浏览器里编辑规则

## 下载安装

到 [Releases](../../releases) 下载对应平台的压缩包。

### macOS 14 及以上

1. 解压 `DownloadsSorter-macOS.zip`，把 `下载整理.app` 拖进「应用程序」。
2. 第一次打开时，系统会提示无法验证开发者（应用没有经过 Apple 公证）。到「系统设置 → 隐私与安全性」，在页面底部点「仍要打开」。
   也可以在终端运行：`xattr -dr com.apple.quarantine /Applications/下载整理.app`
3. 图标出现在菜单栏右上角。如果系统询问能否访问「下载」文件夹，点「允许」。
4. 在菜单里打开「开机自动启动」。

### Windows 10 / 11（64 位）

1. 解压 `DownloadsSorter-Windows.zip`，把 `下载整理.exe` 放到一个固定位置，例如 `D:\Tools\下载整理\`。不要直接在压缩包里运行，也不要放在「下载」文件夹里。
2. 双击运行。如果出现「Windows 已保护你的电脑」，点「更多信息 → 仍要运行」（程序没有代码签名）。
3. 图标出现在任务栏右下角（没看到的话点 `^` 展开）。
4. 点图标，在菜单里勾选「开机自动启动」。

## 默认规则

从上到下匹配，第一条命中的规则生效：

| 条件 | 移到 |
|---|---|
| 文件名含「发票 / invoice」 | `30_财务/发票` |
| 从百望云、诺诺、税务局等发票平台下载 | `30_财务/发票` |
| 文件名含「交易明细 / 账单 / 流水 / 对账单 / statement」 | `30_财务/账单流水` |
| 文件名含「简历 / resume」 | `10_个人档案/简历` |
| 安装程序：`.dmg .pkg .app .exe .msi .msix .vsix` 等 | `90_安装包` |
| 名字像安装包的 `.zip`（含 installer、setup、arm64、x64 等） | `90_安装包` |
| 电子书：`.epub .mobi .azw3` | `40_学习/电子书` |
| 图片：`.jpg .png .heic .webp .avif` 等 | `60_图片` |
| 代码与配置：`.yml .json .conf .sh .py .sql .diff` 等 | `50_代码与配置` |
| 其他文件、文件夹 | `00_收件箱` |

规则可以在应用里修改：一条规则可以同时限定「文件名含」「扩展名」「来源网址含」，填了的条件都要满足；一个条件里写多个值（逗号分隔）时满足其一即可。目标文件夹是相对「下载」的路径，不存在会自动创建。两个平台的 `rules.json` 格式相同，可以互相拷贝。

### 不会动的文件

- 还没下载完的：`.crdownload`、`.download`、`.part`、迅雷 `.xltd`、百度网盘 `.downloading` 等
- 刚到达不满 2 分钟的文件（避免搬走正在写入的文件）；文件夹要 1 小时没有变动、里面没有未完成的下载才处理
- 隐藏文件、`desktop.ini`、Office 的 `~$` 临时文件、正被其他程序打开的文件（Windows）
- 已有的 `00_`～`99_` 分类文件夹，以及规则里用到的目标文件夹
- 目标重名时自动改成 `文件名 (2).扩展名`，不会覆盖

## 文件位置

| | macOS | Windows |
|---|---|---|
| 规则 | `~/Library/Application Support/下载整理/rules.json` | `%APPDATA%\下载整理\rules.json` |
| 日志 | `~/Library/Logs/下载整理.log` | `%APPDATA%\下载整理\下载整理.log` |

## 从源码构建

需要 macOS（Xcode 命令行工具）和 Go 1.24 以上。Windows 版在 Mac 上交叉编译，不需要 Windows 环境。

```sh
./build.sh test              # Mac 版整理逻辑测试
./build.sh                   # 编译 build/下载整理.app
windows/build-windows.sh     # 测试并编译 windows/dist/下载整理.exe
./release.sh                 # 全部测试并打包两个平台到 dist/
```

国内网络可以设置 `GOPROXY=https://goproxy.cn,direct`（`build-windows.sh` 默认已设置）。

## 项目结构

```
Sources/              macOS 应用（SwiftUI）
  Sorter.swift          整理逻辑
  AppModel.swift        监听「下载」、规则存取、开机启动
  Views.swift           菜单栏面板和规则编辑窗口
Tests/main.swift      整理逻辑测试
Tools/make-icon.swift 生成图标
windows/              Windows 版（Go）
  internal/sorter/      整理逻辑（与 Swift 版行为一致，可在 Mac 上测试）
  internal/webui/       浏览器里的规则编辑页，只监听 127.0.0.1 并校验随机令牌
  main_windows.go       托盘菜单、开机启动（当前用户 Run 注册表项）
```
