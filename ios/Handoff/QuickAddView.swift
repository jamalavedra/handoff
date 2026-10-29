import SwiftUI

// Bottom sheet quick add — same text grammar as the web (web/src/quickadd.ts).
struct QuickAddView: View {
    @EnvironmentObject var session: Session
    @Environment(\.dismiss) var dismiss
    var onCreated: () -> Void

    @State private var text = ""
    @State private var members: [QuickRef] = []
    @State private var projects: [QuickRef] = []
    @State private var extId = ""
    @State private var error: String?

    private var parsed: QuickParsed { parseQuickAdd(text, members: members, projects: projects) }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            TextField("Fix the redirect loop tomorrow @marta #Sales !1", text: $text)
                .font(.system(size: 16))
                .autocorrectionDisabled()
                .padding(.horizontal, 14).padding(.vertical, 12)
                .background(Color.hAccent, in: RoundedRectangle(cornerRadius: 12))
            preview
            HStack {
                if let error { Text(error).font(.system(size: 12)).foregroundStyle(Color.hDestructive) }
                Spacer()
                Button("Create") { _Concurrency.Task { await create() } }
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Color.hBg)
                    .disabled(parsed.title.isEmpty)
                    .padding(.horizontal, 18).padding(.vertical, 9)
                    .background(Color.hFg.opacity(parsed.title.isEmpty ? 0.4 : 1), in: Capsule())
            }
            Spacer()
        }
        .padding(16)
        .background(Color.hBg)
        .task { await loadRefs() }
        .onAppear { if extId.isEmpty { extId = "ios-\(UUID().uuidString)" } }
    }

    @ViewBuilder private var preview: some View {
        let p = parsed
        if !text.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text(p.title.isEmpty ? "…" : p.title)
                    .font(.system(size: 15, weight: .medium)).foregroundStyle(Color.hFg)
                HStack(spacing: 8) {
                    if let r = p.repeat { chip(scheduleLabel(r, due: p.due), "repeat") }
                    if let d = p.due { chip(dueText(d), "calendar") }
                    if let a = p.assignee { chip(a.name, "at") }
                    if let pr = p.project { chip(pr.name, "number") }
                    if let pri = p.priority { chip("p\(pri)", "flag.fill") }
                }
            }
        }
    }

    private func chip(_ label: String, _ icon: String) -> some View {
        HStack(spacing: 4) {
            Image(systemName: icon).font(.system(size: 10))
            Text(label)
        }
        .font(.system(size: 11, weight: .medium))
        .foregroundStyle(Color.hMutedFg)
        .padding(.horizontal, 8).padding(.vertical, 3)
        .background(Color.hAccent, in: Capsule())
    }

    private func dueText(_ iso: String) -> String {
        guard let d = isoDay(iso) else { return iso }
        let n = Calendar.current.dateComponents([.day], from: Calendar.current.startOfDay(for: Date()), to: d).day ?? 0
        if n == 0 { return "Today" }
        if n == 1 { return "Tomorrow" }
        return "\(d.formatted(.dateTime.weekday(.abbreviated))) \(DateFormatter.monthDay.string(from: d))"
    }

    private func loadRefs() async {
        guard let c = session.client else { return }
        if let m: MembersOut = try? await c.tool("list_members") {
            members = m.members.filter { $0.kind != "agent" }.map { QuickRef(id: $0.id, name: $0.name) }
        }
        if let p: ProjectsOut = try? await c.tool("list_projects") {
            projects = p.projects.map { QuickRef(id: $0.id, name: $0.name) }
        }
    }

    private func create() async {
        guard let c = session.client else { return }
        let p = parsed
        var t: [String: Any] = ["title": p.title, "external_id": extId]
        if let d = p.due { t["due"] = d }
        if let a = p.assignee { t["assignee"] = a.id }
        if let pr = p.project { t["project"] = pr.id }
        if let pri = p.priority { t["priority"] = pri }
        if let r = p.repeat { t["repeat"] = r }
        do {
            let _: CreateTasksOut = try await c.tool("create_tasks", ["tasks": [t]])
            onCreated()
            dismiss()
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch { self.error = error.localizedDescription }
    }
}
