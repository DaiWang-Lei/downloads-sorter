import Foundation

/// 一条归类规则。填了的条件都要满足；每个条件里有多个值时满足其一即可。
struct Rule: Codable, Identifiable, Hashable {
    var id = UUID()
    var name: String
    var keywords: [String] = []    // 文件名包含（不区分大小写）
    var extensions: [String] = []  // 扩展名
    var sources: [String] = []     // 下载来源网址包含
    var dest: String               // 相对「下载」文件夹的目标目录

    init(_ name: String, keywords: [String] = [], extensions: [String] = [], sources: [String] = [], dest: String) {
        self.name = name
        self.keywords = keywords
        self.extensions = extensions
        self.sources = sources
        self.dest = dest
    }

    // 手改 rules.json 时可以省略 id 和空数组
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decodeIfPresent(UUID.self, forKey: .id) ?? UUID()
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        keywords = try c.decodeIfPresent([String].self, forKey: .keywords) ?? []
        extensions = try c.decodeIfPresent([String].self, forKey: .extensions) ?? []
        sources = try c.decodeIfPresent([String].self, forKey: .sources) ?? []
        dest = try c.decode(String.self, forKey: .dest)
    }

    func matches(fileName: String, ext: String, source: String) -> Bool {
        let kw = keywords.map { $0.lowercased() }.filter { !$0.isEmpty }
        let ex = extensions.map { $0.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: ". ")) }.filter { !$0.isEmpty }
        let sr = sources.map { $0.lowercased() }.filter { !$0.isEmpty }
        if kw.isEmpty && ex.isEmpty && sr.isEmpty { return false }
        let name = fileName.lowercased(), src = source.lowercased()
        if !kw.isEmpty && !kw.contains(where: { name.contains($0) }) { return false }
        if !ex.isEmpty && !ex.contains(ext) { return false }
        if !sr.isEmpty && !sr.contains(where: { src.contains($0) }) { return false }
        return true
    }

    static let defaults: [Rule] = [
        Rule("发票", keywords: ["发票", "invoice"], dest: "30_财务/发票"),
        Rule("发票平台下载", sources: ["baiwang", "fapiao", "chinatax", "nuonuo"], dest: "30_财务/发票"),
        Rule("账单流水", keywords: ["交易明细", "账单", "流水", "对账单", "statement"], dest: "30_财务/账单流水"),
        Rule("简历", keywords: ["简历", "resume"], dest: "10_个人档案/简历"),
        Rule("安装包", extensions: ["dmg", "pkg", "mpkg", "app", "vsix"], dest: "90_安装包"),
        Rule("安装包 zip", keywords: ["installer", "setup", "darwin", "macos", "mac-", "_mac", "arm64", "x64", "universal"],
             extensions: ["zip"], dest: "90_安装包"),
        Rule("电子书", extensions: ["epub", "mobi", "azw3"], dest: "40_学习/电子书"),
        Rule("图片", extensions: ["jpg", "jpeg", "png", "heic", "gif", "webp", "avif", "bmp", "tiff", "svg"], dest: "60_图片"),
        Rule("代码与配置", extensions: ["yml", "yaml", "conf", "toml", "ini", "json", "sh", "py", "sql", "diff", "patch"],
             dest: "50_代码与配置"),
    ]
}

struct MoveRecord: Codable, Identifiable, Hashable {
    var id = UUID()
    let date: Date
    let name: String    // 原文件名
    let target: String  // 移动后相对「下载」的路径
}

struct RunResult {
    var moved: [MoveRecord] = []
    var failures: [String] = []
}

/// 扫描「下载」顶层，把已经静止下来的新文件按规则移走。只移动，不删除。
/// 属性全部只读，每次整理新建一个实例在后台队列上跑。
final class Sorter: @unchecked Sendable {
    static let fallback = "00_收件箱"
    static let partialExts: Set<String> = ["crdownload", "download", "part", "partial", "downloading", "tmp", "opdownload"]

    let root: URL
    let rules: [Rule]
    let settle: TimeInterval     // 文件到达后静止多久才处理，避免搬走正在写入的文件
    let dirSettle: TimeInterval  // 文件夹（如网盘下载）要静止更久
    private let fm = FileManager.default

    init(root: URL, rules: [Rule], settle: TimeInterval = 120, dirSettle: TimeInterval = 3600) {
        self.root = root
        self.rules = rules
        self.settle = settle
        self.dirSettle = dirSettle
    }

    func run(now: Date = Date()) throws -> RunResult {
        let items = try fm.contentsOfDirectory(at: root, includingPropertiesForKeys: [.isDirectoryKey, .attributeModificationDateKey],
                                               options: [.skipsHiddenFiles])
            .sorted { $0.lastPathComponent < $1.lastPathComponent }
        let destRoots = Set(([Self.fallback] + rules.compactMap { Self.safeDest($0.dest) }).compactMap { $0.split(separator: "/").first.map(String.init) })
        var result = RunResult()

        for url in items {
            let name = url.lastPathComponent
            if isManaged(name) || destRoots.contains(name) || isPartial(name) { continue }
            let ext = url.pathExtension.lowercased()
            let isDir = (try? url.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) ?? false
            let dest: String
            if isDir && ext != "app" {
                guard !containsPartial(url), now.timeIntervalSince(newestChange(url)) >= dirSettle else { continue }
                dest = Self.fallback
            } else {
                let changed = isDir ? newestChange(url) : changeDate(url)
                guard now.timeIntervalSince(changed) >= settle else { continue }
                dest = destination(fileName: name, ext: ext, source: whereFroms(url))
            }
            do {
                let dir = root.appendingPathComponent(dest, isDirectory: true)
                try fm.createDirectory(at: dir, withIntermediateDirectories: true)
                let target = uniqueTarget(in: dir, name: name, isDir: isDir)
                try fm.moveItem(at: url, to: target)
                result.moved.append(MoveRecord(date: now, name: name, target: dest + "/" + target.lastPathComponent))
            } catch {
                result.failures.append("\(name) 移动失败：\(error.localizedDescription)")
            }
        }
        return result
    }

    func destination(fileName: String, ext: String, source: String) -> String {
        for rule in rules where rule.matches(fileName: fileName, ext: ext, source: source) {
            if let d = Self.safeDest(rule.dest) { return d }
        }
        return Self.fallback
    }

    /// 只允许「下载」内部的相对路径，拒绝 ..、绝对路径和空值
    static func safeDest(_ s: String) -> String? {
        let parts = s.split(separator: "/").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        if parts.isEmpty || parts.contains(where: { $0 == ".." || $0 == "." }) { return nil }
        return parts.joined(separator: "/")
    }

    /// 已整理好的分类目录，如 00_收件箱、60_图片
    func isManaged(_ name: String) -> Bool {
        let c = Array(name.prefix(3))
        return c.count == 3 && c[0].isASCII && c[0].isNumber && c[1].isASCII && c[1].isNumber && c[2] == "_"
    }

    func isPartial(_ name: String) -> Bool {
        Self.partialExts.contains((name as NSString).pathExtension.lowercased())
    }

    /// 「下载」里的文件会被浏览器写入来源网址（与 Finder「显示简介」里的「来源」相同）
    func whereFroms(_ url: URL) -> String {
        let attr = "com.apple.metadata:kMDItemWhereFroms"
        let size = getxattr(url.path, attr, nil, 0, 0, 0)
        guard size > 0 else { return "" }
        var data = Data(count: size)
        let read = data.withUnsafeMutableBytes { getxattr(url.path, attr, $0.baseAddress, size, 0, 0) }
        guard read > 0, let list = try? PropertyListSerialization.propertyList(from: data, format: nil) as? [String] else { return "" }
        return list.joined(separator: " ")
    }

    // attributeModificationDate 即 ctime：下载完成改名、被移入时都会更新
    private func changeDate(_ url: URL) -> Date {
        (try? url.resourceValues(forKeys: [.attributeModificationDateKey]).attributeModificationDate) ?? .distantPast
    }

    private func newestChange(_ dir: URL) -> Date {
        var newest = changeDate(dir)
        if let e = fm.enumerator(at: dir, includingPropertiesForKeys: [.attributeModificationDateKey]) {
            for case let u as URL in e { newest = max(newest, changeDate(u)) }
        }
        return newest
    }

    private func containsPartial(_ dir: URL) -> Bool {
        guard let e = fm.enumerator(at: dir, includingPropertiesForKeys: nil) else { return false }
        for case let u as URL in e where isPartial(u.lastPathComponent) { return true }
        return false
    }

    private func uniqueTarget(in dir: URL, name: String, isDir: Bool) -> URL {
        let ext = (name as NSString).pathExtension, stem = (name as NSString).deletingPathExtension
        var target = dir.appendingPathComponent(name)
        var i = 2
        while fm.fileExists(atPath: target.path) {
            let n = (!isDir && !ext.isEmpty && !stem.isEmpty) ? "\(stem) (\(i)).\(ext)" : "\(name) (\(i))"
            target = dir.appendingPathComponent(n)
            i += 1
        }
        return target
    }
}
