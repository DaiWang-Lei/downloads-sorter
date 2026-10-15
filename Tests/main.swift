import Foundation

// 在临时目录里模拟「下载」文件夹，验证 Sorter 的行为。用法: ./build.sh test
let fm = FileManager.default
let root = fm.temporaryDirectory.appendingPathComponent("sorter-test-\(UUID().uuidString)")
defer { try? fm.removeItem(at: root) }
var failed = 0

func touch(_ path: String, _ text: String = "") {
    let u = root.appendingPathComponent(path)
    try! fm.createDirectory(at: u.deletingLastPathComponent(), withIntermediateDirectories: true)
    try! Data(text.utf8).write(to: u)
}
func mkdir(_ path: String) { try! fm.createDirectory(at: root.appendingPathComponent(path), withIntermediateDirectories: true) }
func exists(_ path: String) -> Bool { fm.fileExists(atPath: root.appendingPathComponent(path).path) }
func check(_ cond: Bool, _ msg: String) {
    print(cond ? "  ✓ \(msg)" : "  ✗ \(msg)")
    if !cond { failed += 1 }
}
func setWhereFroms(_ path: String, _ urls: [String]) {
    let data = try! PropertyListSerialization.data(fromPropertyList: urls, format: .binary, options: 0)
    _ = data.withUnsafeBytes { setxattr(root.appendingPathComponent(path).path, "com.apple.metadata:kMDItemWhereFroms", $0.baseAddress, data.count, 0, 0) }
}

mkdir("10_个人档案"); mkdir("60_图片"); mkdir("课程"); mkdir("资料包"); mkdir("Some.app/Contents")
for f in ["发票_20261014.pdf", "invoice-123.PDF", "支付宝交易明细(20261001).pdf", "张三简历.pdf", "Foo-1.0-arm64.dmg",
          "Qoder-Installer-mac-arm64.zip", "random.zip", "book.EPUB", "photo.HEIC", "60_图片/photo.HEIC", "docker-compose.yml",
          "report.pdf", "noext", "big.mp4.crdownload", "my 'file' (1).png", ".DS_Store", "课程/a.mp4.baiduyun.p.downloading",
          "资料包/a.txt", "张三-61-20261021.pdf", "演示.mp4"] { touch(f) }
setWhereFroms("张三-61-20261021.pdf", ["https://bwfp.baiwang.com/invoiceInfo/download?x=1", "https://bwfp.baiwang.com/fp/detail"])

print("1) 刚到达的文件全部跳过")
var r = try! Sorter(root: root, rules: Rule.defaults).run()
check(r.moved.isEmpty, "没有移动任何文件（实际 \(r.moved.count)）")

print("2) 静止后按规则归类")
let custom = Rule.defaults + [Rule("视频", extensions: ["mp4"], dest: "视频"), Rule("越界", keywords: ["report"], dest: "../evil")]
r = try! Sorter(root: root, rules: custom, settle: 0, dirSettle: 0).run()
for m in r.moved { print("     \(m.name) → \(m.target)") }
let expect: [String: String] = [
    "30_财务/发票/发票_20261014.pdf": "文件名含发票",
    "30_财务/发票/invoice-123.PDF": "invoice 不区分大小写",
    "30_财务/发票/张三-61-20261021.pdf": "按下载来源 baiwang 识别发票",
    "30_财务/账单流水/支付宝交易明细(20261001).pdf": "账单",
    "10_个人档案/简历/张三简历.pdf": "简历",
    "90_安装包/Foo-1.0-arm64.dmg": "dmg",
    "90_安装包/Qoder-Installer-mac-arm64.zip": "安装包 zip",
    "90_安装包/Some.app": ".app 文件夹按安装包处理",
    "00_收件箱/random.zip": "普通 zip 进收件箱",
    "40_学习/电子书/book.EPUB": "扩展名不区分大小写",
    "60_图片/photo (2).HEIC": "重名自动加 (2)",
    "60_图片/photo.HEIC": "原有同名文件不被覆盖",
    "60_图片/my 'file' (1).png": "特殊字符文件名",
    "50_代码与配置/docker-compose.yml": "配置文件",
    "00_收件箱/report.pdf": "越界目标 ../evil 被拒绝，退回收件箱",
    "00_收件箱/noext": "无扩展名进收件箱",
    "00_收件箱/资料包": "已完成的文件夹进收件箱",
    "视频/演示.mp4": "自定义规则和目标目录",
]
for (path, why) in expect.sorted(by: { $0.key < $1.key }) { check(exists(path), "\(why): \(path)") }
check(exists("big.mp4.crdownload"), "未下载完的 .crdownload 不动")
check(exists("课程/a.mp4.baiduyun.p.downloading"), "含未完成下载的文件夹不动")
check(exists(".DS_Store"), "隐藏文件不动")
check(!fm.fileExists(atPath: root.deletingLastPathComponent().appendingPathComponent("evil").path), "没有写到「下载」外面")
check(r.failures.isEmpty, "没有失败项 \(r.failures)")

print("3) 再跑一次：自定义目录「视频」不会被当成新文件夹移走")
r = try! Sorter(root: root, rules: custom, settle: 0, dirSettle: 0).run()
check(r.moved.isEmpty, "没有新的移动（实际 \(r.moved.map(\.name))）")

print("4) 手改的 rules.json 可以省略 id 和空字段")
let json = #"[{"name":"测试","extensions":["mp3"],"dest":"70_音频"}]"#
let decoded = try? JSONDecoder().decode([Rule].self, from: Data(json.utf8))
check(decoded?.first?.dest == "70_音频" && decoded?.first?.keywords == [], "解析成功")

print(failed == 0 ? "\n全部通过" : "\n\(failed) 项失败")
exit(failed == 0 ? 0 : 1)
