import AppKit
import ServiceManagement
import SwiftUI

@MainActor
final class AppModel: ObservableObject {
    @Published var enabled: Bool {
        didSet {
            defaults.set(enabled, forKey: "enabled")
            enabled ? start() : stop()
        }
    }
    @Published var rules: [Rule] { didSet { saveRules() } }
    @Published private(set) var recent: [MoveRecord] = []
    @Published private(set) var inboxCount = 0
    @Published private(set) var lastRun: Date?
    @Published private(set) var errorText: String?
    @Published private(set) var needsPermission = false
    @Published private(set) var launchAtLogin = SMAppService.mainApp.status == .enabled

    let root = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Downloads", isDirectory: true)
    static let supportDir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        .appendingPathComponent("下载整理", isDirectory: true)
    static let rulesFile = supportDir.appendingPathComponent("rules.json")
    static let logFile = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Logs/下载整理.log")

    private let defaults = UserDefaults.standard
    private let queue = DispatchQueue(label: "downloads-sorter")
    private var watcher: DispatchSourceFileSystemObject?
    private var timer: Timer?
    private var debounce: DispatchWorkItem?
    private var rulesWindow: NSWindow?

    init() {
        enabled = defaults.object(forKey: "enabled") as? Bool ?? true
        rules = Self.loadRules()
        if let data = defaults.data(forKey: "recent"), let r = try? JSONDecoder().decode([MoveRecord].self, from: data) {
            recent = r
        }
        refreshInbox()
        if enabled { start() }
    }

    // MARK: 监听与整理

    /// 「下载」有变动时（3 秒防抖）整理一次；另外每分钟兜底一次，处理之前因为还没静止而跳过的文件
    private func start() {
        stop()
        let fd = open(root.path, O_EVTONLY)
        if fd >= 0 {
            let source = DispatchSource.makeFileSystemObjectSource(fileDescriptor: fd, eventMask: [.write, .rename, .delete], queue: .main)
            source.setEventHandler { [weak self] in MainActor.assumeIsolated { self?.scheduleRun() } }
            source.setCancelHandler { close(fd) }
            source.resume()
            watcher = source
        }
        timer = Timer.scheduledTimer(withTimeInterval: 60, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.runNow() }
        }
        runNow()
    }

    private func stop() {
        watcher?.cancel()
        watcher = nil
        timer?.invalidate()
        timer = nil
    }

    private func scheduleRun() {
        debounce?.cancel()
        let work = DispatchWorkItem { [weak self] in MainActor.assumeIsolated { self?.runNow() } }
        debounce = work
        DispatchQueue.main.asyncAfter(deadline: .now() + 3, execute: work)
    }

    /// settle: 文件到达后静止多少秒才处理。手动「立即整理」用更短的等待
    func runNow(settle: TimeInterval = 120) {
        let sorter = Sorter(root: root, rules: rules, settle: settle)
        queue.async { [weak self] in
            let result = Result { try sorter.run() }
            DispatchQueue.main.async { MainActor.assumeIsolated { self?.finish(result) } }
        }
    }

    private func finish(_ result: Result<RunResult, Error>) {
        lastRun = Date()
        switch result {
        case .success(let r):
            needsPermission = false
            errorText = r.failures.first
            if !r.moved.isEmpty {
                recent = Array((r.moved.reversed() + recent).prefix(30))
                defaults.set(try? JSONEncoder().encode(recent), forKey: "recent")
            }
            appendLog(r.moved.map { "\($0.name) → \($0.target)" } + r.failures)
        case .failure(let error):
            let ns = error as NSError
            needsPermission = ns.domain == NSCocoaErrorDomain && ns.code == NSFileReadNoPermissionError
            errorText = needsPermission ? "没有访问「下载」文件夹的权限" : "读取「下载」失败：\(error.localizedDescription)"
            appendLog([errorText!])
        }
        refreshInbox()
    }

    private func refreshInbox() {
        let inbox = root.appendingPathComponent(Sorter.fallback)
        inboxCount = (try? FileManager.default.contentsOfDirectory(at: inbox, includingPropertiesForKeys: nil, options: [.skipsHiddenFiles]))?.count ?? 0
    }

    private func appendLog(_ lines: [String]) {
        guard !lines.isEmpty else { return }
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm:ss"
        let stamp = f.string(from: Date())
        let data = Data(lines.map { "\(stamp)  \($0)\n" }.joined().utf8)
        if let h = try? FileHandle(forWritingTo: Self.logFile) {
            h.seekToEndOfFile()
            h.write(data)
            try? h.close()
        } else {
            try? data.write(to: Self.logFile)
        }
    }

    // MARK: 规则

    static func loadRules() -> [Rule] {
        guard let data = try? Data(contentsOf: rulesFile), let rules = try? JSONDecoder().decode([Rule].self, from: data) else {
            return Rule.defaults
        }
        return rules
    }

    private func saveRules() {
        try? FileManager.default.createDirectory(at: Self.supportDir, withIntermediateDirectories: true)
        let enc = JSONEncoder()
        enc.outputFormatting = [.prettyPrinted, .withoutEscapingSlashes]
        try? enc.encode(rules).write(to: Self.rulesFile, options: .atomic)
    }

    func moveRule(_ id: UUID, by offset: Int) {
        guard let i = rules.firstIndex(where: { $0.id == id }), rules.indices.contains(i + offset) else { return }
        rules.swapAt(i, i + offset)
    }

    func deleteRule(_ id: UUID) { rules.removeAll { $0.id == id } }

    // MARK: 开机启动

    func setLaunchAtLogin(_ on: Bool) {
        do {
            if on { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
        } catch {
            errorText = "开机启动设置失败：\(error.localizedDescription)"
        }
        if SMAppService.mainApp.status == .requiresApproval {
            errorText = "请在「系统设置 → 通用 → 登录项」里允许「下载整理」"
            SMAppService.openSystemSettingsLoginItems()
        }
        launchAtLogin = SMAppService.mainApp.status == .enabled
    }

    // MARK: 打开文件/窗口

    func openFolder(_ relative: String) {
        let url = root.appendingPathComponent(relative, isDirectory: true)
        try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        NSWorkspace.shared.open(url)
    }

    func reveal(_ record: MoveRecord) {
        let url = root.appendingPathComponent(record.target)
        if FileManager.default.fileExists(atPath: url.path) {
            NSWorkspace.shared.activateFileViewerSelecting([url])
        } else {
            NSWorkspace.shared.open(url.deletingLastPathComponent())
        }
    }

    func openLog() {
        if !FileManager.default.fileExists(atPath: Self.logFile.path) { appendLog(["日志已创建"]) }
        NSWorkspace.shared.open(Self.logFile)
    }

    func openPrivacySettings() {
        NSWorkspace.shared.open(URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_FilesAndFolders")!)
    }

    func showRules() {
        if rulesWindow == nil {
            let window = NSWindow(contentViewController: NSHostingController(rootView: RulesView().environmentObject(self)))
            window.title = "整理规则"
            window.styleMask = [.titled, .closable, .resizable, .miniaturizable]
            window.setContentSize(NSSize(width: 780, height: 600))
            window.isReleasedWhenClosed = false
            window.center()
            rulesWindow = window
        }
        NSApp.activate(ignoringOtherApps: true)
        rulesWindow?.makeKeyAndOrderFront(nil)
    }
}
