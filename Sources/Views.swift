import SwiftUI

@main
struct DownloadsSorterApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        MenuBarExtra {
            MenuView().environmentObject(model)
        } label: {
            Image(systemName: model.enabled ? "tray.and.arrow.down.fill" : "tray.and.arrow.down")
        }
        .menuBarExtraStyle(.window)
    }
}

struct MenuView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("下载整理").font(.headline)
                Spacer()
                Toggle("自动整理", isOn: $model.enabled).toggleStyle(.switch).controlSize(.small)
            }

            if let error = model.errorText {
                VStack(alignment: .leading, spacing: 6) {
                    Label(error, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.orange)
                        .font(.callout)
                        .fixedSize(horizontal: false, vertical: true)
                    if model.needsPermission {
                        Button("打开隐私设置") { model.openPrivacySettings() }.controlSize(.small)
                    }
                }
            }

            HStack {
                Button { model.openFolder(Sorter.fallback) } label: {
                    Label("收件箱 \(model.inboxCount) 项待整理", systemImage: "tray")
                }
                .buttonStyle(.link)
                Spacer()
                Button("立即整理") { model.runNow(settle: 10) }
            }

            Divider()

            Text("最近整理").font(.subheadline).foregroundStyle(.secondary)
            if model.recent.isEmpty {
                Text("还没有整理记录").font(.callout).foregroundStyle(.tertiary)
            }
            ForEach(model.recent.prefix(8)) { record in
                Button { model.reveal(record) } label: {
                    VStack(alignment: .leading, spacing: 1) {
                        Text(record.name).lineLimit(1).truncationMode(.middle)
                        Text("→ \(record.target)")
                            .font(.caption).foregroundStyle(.secondary)
                            .lineLimit(1).truncationMode(.middle)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("在访达中显示")
            }

            Divider()

            Toggle("开机自动启动", isOn: Binding(get: { model.launchAtLogin }, set: { model.setLaunchAtLogin($0) }))
            HStack {
                Button("编辑规则…") { model.showRules() }
                Button("打开日志") { model.openLog() }
                Spacer()
                Button("退出") { NSApp.terminate(nil) }
            }
            if let last = model.lastRun {
                Text("上次检查 \(last.formatted(date: .omitted, time: .standard))")
                    .font(.caption2).foregroundStyle(.tertiary)
            }
        }
        .padding(14)
        .frame(width: 340)
    }
}

struct RulesView: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("从上到下匹配，第一条命中的规则生效；都不命中的放进 00_收件箱。同一条规则里填了的条件都要满足；一个条件里写多个值（逗号分隔）时满足其一即可。")
                .font(.callout).foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            ScrollView {
                VStack(spacing: 10) {
                    ForEach(model.rules) { rule in
                        RuleRow(rule: binding(for: rule.id),
                                isFirst: rule.id == model.rules.first?.id,
                                isLast: rule.id == model.rules.last?.id,
                                onMove: { model.moveRule(rule.id, by: $0) },
                                onDelete: { model.deleteRule(rule.id) })
                    }
                }
                .padding(.trailing, 4)
            }
            HStack {
                Button("添加规则") { model.rules.append(Rule("新规则", dest: Sorter.fallback)) }
                Button("恢复默认") { model.rules = Rule.defaults }
                Spacer()
                Text("目标文件夹是相对「下载」的路径，不存在会自动创建").font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(16)
        .frame(minWidth: 720, minHeight: 480)
    }

    private func binding(for id: UUID) -> Binding<Rule> {
        Binding(
            get: { model.rules.first { $0.id == id } ?? Rule("", dest: "") },
            set: { new in
                if let i = model.rules.firstIndex(where: { $0.id == id }) { model.rules[i] = new }
            })
    }
}

struct RuleRow: View {
    @Binding var rule: Rule
    let isFirst: Bool
    let isLast: Bool
    let onMove: (Int) -> Void
    let onDelete: () -> Void

    // 逗号分隔的输入框单独保存原文，避免边输入边被重新格式化
    @State private var keywords: String
    @State private var extensions: String
    @State private var sources: String

    init(rule: Binding<Rule>, isFirst: Bool, isLast: Bool, onMove: @escaping (Int) -> Void, onDelete: @escaping () -> Void) {
        _rule = rule
        self.isFirst = isFirst
        self.isLast = isLast
        self.onMove = onMove
        self.onDelete = onDelete
        _keywords = State(initialValue: rule.wrappedValue.keywords.joined(separator: ", "))
        _extensions = State(initialValue: rule.wrappedValue.extensions.joined(separator: ", "))
        _sources = State(initialValue: rule.wrappedValue.sources.joined(separator: ", "))
    }

    var body: some View {
        GroupBox {
            HStack(alignment: .top, spacing: 12) {
                Grid(alignment: .leading, horizontalSpacing: 8, verticalSpacing: 6) {
                    GridRow {
                        Text("名称").foregroundStyle(.secondary)
                        TextField("", text: $rule.name)
                        Text("移到").foregroundStyle(.secondary)
                        TextField("如 60_图片", text: $rule.dest)
                    }
                    GridRow {
                        Text("文件名含").foregroundStyle(.secondary)
                        TextField("关键词，逗号分隔", text: $keywords).gridCellColumns(3)
                    }
                    GridRow {
                        Text("扩展名").foregroundStyle(.secondary)
                        TextField("如 jpg, png", text: $extensions).gridCellColumns(3)
                    }
                    GridRow {
                        Text("来源网址含").foregroundStyle(.secondary)
                        TextField("如 baiwang（浏览器记录的下载来源）", text: $sources).gridCellColumns(3)
                    }
                }
                VStack(spacing: 4) {
                    Button { onMove(-1) } label: { Image(systemName: "chevron.up") }.disabled(isFirst).help("上移")
                    Button { onMove(1) } label: { Image(systemName: "chevron.down") }.disabled(isLast).help("下移")
                    Button(role: .destructive, action: onDelete) { Image(systemName: "trash") }.help("删除这条规则")
                }
                .buttonStyle(.borderless)
            }
            .padding(4)
        }
        .onChange(of: keywords) { _, v in rule.keywords = Self.split(v) }
        .onChange(of: extensions) { _, v in rule.extensions = Self.split(v) }
        .onChange(of: sources) { _, v in rule.sources = Self.split(v) }
    }

    static func split(_ s: String) -> [String] {
        s.split(whereSeparator: { ",，、".contains($0) })
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }
}
