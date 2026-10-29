import SwiftUI

// Signed-in shell: page content + the web's floating bottom tab bar.
struct MainView: View {
    @EnvironmentObject var session: Session
    @State private var tab = Tab.tasks
    @State private var unread = 0
    @State private var showAdd = false
    @State private var showMore = false
    @State private var tasksReload = 0

    enum Tab { case tasks, inbox, projects }

    var body: some View {
        ZStack(alignment: .bottom) {
            // Each tab gets its own navigation stack — switching tabs goes to that
            // tab's root, like the web's separate routes.
            switch tab {
            case .tasks: tabStack(TasksView().id(tasksReload))
            case .inbox: tabStack(InboxView(unread: $unread))
            case .projects: tabStack(ProjectsView())
            }
            tabBar
        }
        .background(Color.hBg)
        .sheet(isPresented: $showAdd) {
            QuickAddView { tasksReload += 1 }
                .presentationDetents([.height(240)])
        }
        .sheet(isPresented: $showMore) {
            MoreView()
                .presentationDetents([.height(260)])
        }
        .task(id: tab) { await refreshUnread() }
    }

    private func tabStack<Content: View>(_ content: Content) -> some View {
        NavigationStack {
            content
                .navigationBarTitleDisplayMode(.inline)
                .navigationDestination(for: String.self) { v in
                    if v.hasPrefix("project:") {
                        TasksView(projectFilter: String(v.dropFirst(8)), title: String(v.dropFirst(8)))
                    } else {
                        TaskDetailView(id: v)
                    }
                }
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        Text(session.me?.org?.name ?? "")
                            .font(.system(size: 14, weight: .medium))
                            .fixedSize()
                            .padding(.horizontal, 12).padding(.vertical, 6)
                            .overlay(Capsule().strokeBorder(Color.hBorder, lineWidth: 0.5))
                            .background(Color.hCard, in: Capsule())
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        if tab == .tasks {
                            Button { showAdd = true } label: {
                                Image(systemName: "plus").font(.system(size: 15, weight: .medium))
                                    .frame(width: 34, height: 34)
                                    .background(Color.hCard, in: Circle())
                                    .overlay(Circle().strokeBorder(Color.hBorder, lineWidth: 0.5))
                            }
                        }
                    }
                }
        }
    }

    private var tabBar: some View {
        HStack(spacing: 4) {
            tabItem(.tasks, "checklist", "Tasks")
            tabItem(.inbox, "tray", "Inbox", badge: unread)
            tabItem(.projects, "folder", "Projects")
            Button { showMore = true } label: {
                tabLabel("ellipsis", "More", active: false)
            }
        }
        .padding(5)
        .background(.ultraThinMaterial.opacity(0.95), in: RoundedRectangle(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Color.hBorder, lineWidth: 0.5))
        .shadow(color: .black.opacity(0.15), radius: 12, y: 4)
        .padding(.horizontal, 12)
        .padding(.bottom, 8)
    }

    private func tabItem(_ t: Tab, _ icon: String, _ label: String, badge: Int = 0) -> some View {
        Button { tab = t } label: {
            tabLabel(icon, label, active: tab == t, badge: badge)
        }
    }

    private func tabLabel(_ icon: String, _ label: String, active: Bool, badge: Int = 0) -> some View {
        VStack(spacing: 3) {
            ZStack(alignment: .topTrailing) {
                Image(systemName: icon).font(.system(size: 19))
                if badge > 0 {
                    Text("\(min(badge, 99))")
                        .font(.system(size: 9, weight: .bold)).monospacedDigit()
                        .foregroundStyle(.white)
                        .padding(.horizontal, 4).padding(.vertical, 1)
                        .background(Color.hUnseen, in: Capsule())
                        .offset(x: 10, y: -6)
                }
            }
            Text(label).font(.system(size: 11, weight: .medium))
        }
        .foregroundStyle(active ? Color.hFg : Color.hFg.opacity(0.5))
        .frame(maxWidth: .infinity)
        .padding(.vertical, 7)
        .background(active ? Color.hAccent : .clear, in: RoundedRectangle(cornerRadius: 12))
    }

    private func refreshUnread() async {
        guard let c = session.client else { return }
        if let out: InboxOut = try? await c.tool("inbox", ["limit": 1]) {
            unread = out.unread
        }
    }
}

// Inbox mirrors pages/notifications.tsx.
struct InboxView: View {
    @EnvironmentObject var session: Session
    @Binding var unread: Int
    @State private var items: [Notification] = []
    @State private var error: String?

    var body: some View {
        ScrollView {
            LazyVStack(spacing: 6) {
                if let error { Text(error).foregroundStyle(Color.hDestructive).padding() }
                ForEach(items) { n in
                    NavigationLink(value: n.task_id) { row(n) }.buttonStyle(.plain)
                }
                if items.isEmpty && error == nil {
                    Text("All caught up.").foregroundStyle(Color.hMutedFg).padding()
                }
            }
            .padding(.horizontal, 12)
        }
        .background(Color.hBg)
        .safeAreaInset(edge: .bottom) { Color.clear.frame(height: 76) }
        .navigationTitle("Inbox")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                if items.contains(where: { !$0.read }) {
                    Button("Mark all read") { _Concurrency.Task { await markAll() } }
                        .font(.system(size: 13))
                }
            }
        }
        .refreshable { await load() }
        .task { await load(); await markAll() }
    }

    private func row(_ n: Notification) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Avatar(member: n.actor, size: 28)
            VStack(alignment: .leading, spacing: 3) {
                Text(headline(n))
                    .font(.system(size: 14))
                    .foregroundStyle(Color.hFg)
                    .lineLimit(2)
                if let x = n.excerpt, !x.isEmpty {
                    Text(x).font(.system(size: 13)).foregroundStyle(Color.hMutedFg).lineLimit(2)
                }
                Text(ago(n.at)).font(.system(size: 12)).foregroundStyle(Color.hMutedFg.opacity(0.7))
            }
            Spacer(minLength: 4)
            if !n.read { Circle().fill(Color.hUnseen).frame(width: 8, height: 8).padding(.top, 6) }
        }
        .padding(12)
        .background(Color.hCard, in: RoundedRectangle(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Color.hBorder, lineWidth: 0.5))
    }

    private func headline(_ n: Notification) -> String {
        let who = n.actor?.name ?? "Someone"
        switch n.kind {
        case "mention": return "\(who) mentioned you in \(n.task_title)"
        case "assigned": return "\(who) assigned you \(n.task_title)"
        case "approval": return "\(n.task_title) needs your approval"
        case "due": return "\(n.task_title) is due"
        case "agent_done": return "\(who) finished \(n.task_title)"
        default: return "\(who) — \(n.task_title)"
        }
    }

    private func load() async {
        guard let c = session.client else { return }
        do {
            let out: InboxOut = try await c.tool("inbox", ["limit": 50])
            items = out.notifications
            unread = out.unread
            error = nil
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch { self.error = error.localizedDescription }
    }

    private func markAll() async {
        guard let c = session.client, items.contains(where: { !$0.read }) else { return }
        let _: [String: Int]? = try? await c.tool("mark_read", ["all": true])
        for i in items.indices { items[i].read = true }
        unread = 0
    }
}

// Projects mirrors list_projects + drill-in.
struct ProjectsView: View {
    @EnvironmentObject var session: Session
    @State private var projects: [Project] = []
    @State private var error: String?

    var body: some View {
        ScrollView {
            LazyVStack(spacing: 6) {
                if let error { Text(error).foregroundStyle(Color.hDestructive).padding() }
                ForEach(projects) { p in
                    NavigationLink(value: "project:\(p.name)") { row(p) }.buttonStyle(.plain)
                }
                if projects.isEmpty && error == nil {
                    Text("No projects yet.").foregroundStyle(Color.hMutedFg).padding()
                }
            }
            .padding(.horizontal, 12)
        }
        .background(Color.hBg)
        .safeAreaInset(edge: .bottom) { Color.clear.frame(height: 76) }
        .navigationTitle("Projects")
        .refreshable { await load() }
        .task { await load() }
    }

    private func row(_ p: Project) -> some View {
        HStack(spacing: 12) {
            Text(p.emoji ?? "").font(.system(size: 18))
            if p.emoji == nil { Image(systemName: "folder").foregroundStyle(Color.hMutedFg) }
            Text(p.name).font(.system(size: 15, weight: .medium)).foregroundStyle(Color.hFg)
            if p.visibility == "private" {
                Image(systemName: "lock").font(.system(size: 11)).foregroundStyle(Color.hMutedFg)
            }
            Spacer()
            Text("\(p.open_tasks)").font(.system(size: 13, weight: .medium)).monospacedDigit()
                .foregroundStyle(Color.hMutedFg)
            Image(systemName: "chevron.right").font(.system(size: 11)).foregroundStyle(Color.hMutedFg.opacity(0.5))
        }
        .frame(minHeight: 52)
        .padding(.horizontal, 12)
        .background(Color.hCard, in: RoundedRectangle(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Color.hBorder, lineWidth: 0.5))
    }

    private func load() async {
        guard let c = session.client else { return }
        do {
            let out: ProjectsOut = try await c.tool("list_projects")
            projects = out.projects
            error = nil
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch { self.error = error.localizedDescription }
    }
}

// More drawer — account + sign out, like the web More sheet.
struct MoreView: View {
    @EnvironmentObject var session: Session
    @Environment(\.dismiss) var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let m = session.me?.member {
                HStack(spacing: 12) {
                    Avatar(member: MemberRef(id: m.id, name: m.name, kind: "human"), size: 40)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(m.name).font(.system(size: 16, weight: .semibold)).foregroundStyle(Color.hFg)
                        if let email = m.email { Text(email).font(.system(size: 13)).foregroundStyle(Color.hMutedFg) }
                    }
                }
                .padding(16)
            }
            if let base = session.client?.base.host {
                // host:port (e.g. localhost:8081) — port matters on dev servers.
                let port = session.client?.base.port.map { ":\($0)" } ?? ""
                Text(base + port).font(.system(size: 12)).foregroundStyle(Color.hMutedFg).padding(.horizontal, 16)
            }
            Divider().padding(.vertical, 8)
            Button(role: .destructive) {
                session.signOut()
                dismiss()
            } label: {
                Label("Sign out", systemImage: "rectangle.portrait.and.arrow.right")
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(Color.hDestructive)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16).padding(.vertical, 10)
            }
            Spacer()
        }
        .padding(.top, 8)
        .background(Color.hBg)
    }
}
