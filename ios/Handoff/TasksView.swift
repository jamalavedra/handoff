import SwiftUI

// Open work grouped by due, mirroring pages/tasks.tsx at phone width.
struct TasksView: View {
    @EnvironmentObject var session: Session
    @State var scope = Scope.all
    @State var tasks: [Task] = []
    @State var error: String?
    var projectFilter: String? = nil      // Projects drill-in passes a project name
    var title = "Tasks"

    enum Scope: String, CaseIterable, Identifiable {
        case all = "All", mine = "Mine", ready = "Ready"
        var id: Self { self }
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                if let error { Text(error).foregroundStyle(Color.hDestructive).padding() }
                ForEach(groupByDue(tasks)) { section in
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(section.title)
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundStyle(section.key == "overdue" ? Color.hDestructive : Color.hMutedFg)
                        Text("\(section.tasks.count)")
                            .font(.system(size: 12))
                            .foregroundStyle(Color.hMutedFg.opacity(0.5))
                    }
                    .padding(.top, 20).padding(.bottom, 8).padding(.horizontal, 4)
                    ForEach(section.tasks) { t in
                        NavigationLink(value: t.id) { TaskRow(t: t) }
                            .buttonStyle(.plain)
                            .padding(.bottom, 6)
                    }
                }
                if tasks.isEmpty && error == nil {
                    Text("Nothing here.").foregroundStyle(Color.hMutedFg).padding()
                }
            }
            .padding(.horizontal, 12)
        }
        .scrollContentBackground(.hidden)
        .background(Color.hBg)
        .safeAreaInset(edge: .bottom) { Color.clear.frame(height: 76) } // clear of the floating tab bar
        .navigationTitle(title)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    Picker("Scope", selection: $scope) {
                        ForEach(Scope.allCases) { Text($0.rawValue).tag($0) }
                    }
                } label: {
                    Image(systemName: scope == .all ? "line.3.horizontal.decrease.circle" : "line.3.horizontal.decrease.circle.fill")
                        .font(.system(size: 15, weight: .medium))
                        .frame(width: 34, height: 34)
                        .background(Color.hCard, in: Circle())
                        .overlay(Circle().strokeBorder(Color.hBorder, lineWidth: 0.5))
                }
            }
        }
        .refreshable { await load() }
        .task { await load() }
        .task(id: scope) { await load() }
    }

    func load() async {
        guard let c = session.client else { return }
        do {
            if scope == .ready {
                var args: [String: Any] = ["limit": 100]
                if let p = projectFilter { args["project"] = p }
                let out: TasksOut = try await c.tool("list_ready", args)
                tasks = out.tasks
            } else {
                var all: [Task] = []
                var offset = 0
                while true {
                    var args: [String: Any] = ["limit": 100, "offset": offset]
                    if scope == .mine { args["assignee"] = "me" }
                    let page: TasksOut = try await c.tool("search_tasks", args)
                    all += page.tasks
                    if page.tasks.count < 100 { break }
                    offset += 100
                }
                tasks = projectFilter == nil ? all : all.filter { $0.project == projectFilter }
            }
            error = nil
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch {
            self.error = error.localizedDescription
        }
    }
}

// TaskRow mirrors pages/tasks.tsx: status circle | title cluster | right cluster.
struct TaskRow: View {
    @EnvironmentObject var session: Session
    var t: Task

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            StatusCircle(task: t)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    if t.unseen == true {
                        Circle().fill(Color.hUnseen).frame(width: 8, height: 8)
                    }
                    Text(t.title)
                        .font(.system(size: 15, weight: .medium))
                        .lineLimit(1)
                        .strikethrough(t.closed)
                        .foregroundStyle(t.closed ? Color.hMutedFg : Color.hFg)
                    if t.priority <= 1 {
                        Image(systemName: "flag.fill")
                            .font(.system(size: 11))
                            .foregroundStyle(t.priority == 0 ? Color.hDestructive : Color.orange)
                    }
                }
                if let note = t.agent_note, !note.isEmpty {
                    HStack(spacing: 4) {
                        Image(systemName: "cpu").font(.system(size: 12)).foregroundStyle(Color.hViolet)
                        Text(note.components(separatedBy: "\n").first ?? note)
                            .font(.system(size: 13)).lineLimit(1).foregroundStyle(Color.hMutedFg)
                    }
                }
            }
            Spacer(minLength: 6)
            rightCluster
        }
        .frame(minHeight: 56)
        .padding(.horizontal, 12)
        .background(Color.hCard, in: RoundedRectangle(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Color.hBorder, lineWidth: 0.5))
    }

    @ViewBuilder private var rightCluster: some View {
        if let waiting = t.waiting_on {
            let me = waiting.id == session.me?.member?.id
            HStack(spacing: 4) {
                Image(systemName: "clock").font(.system(size: 10))
                Text(me ? "Needs you" : "Needs \(waiting.name.split(separator: " ").first.map(String.init) ?? waiting.name)")
            }
            .font(.system(size: 11, weight: .medium))
            .foregroundStyle(Color.hAmberFg)
            .padding(.horizontal, 8).padding(.vertical, 3)
            .background(Color.hAmber.opacity(0.12), in: Capsule())
        } else if let claimed = t.claimed_by, !t.closed {
            HStack(spacing: 5) {
                Circle().fill(claimed.kind == "agent" ? Color.hViolet : Color.hAmber).frame(width: 6, height: 6)
                Text(claimed.kind == "agent" ? "on it" : claimed.name.split(separator: " ").first.map(String.init) ?? claimed.name)
            }
            .font(.system(size: 11, weight: .medium))
            .foregroundStyle(Color.hFg.opacity(0.8))
            .padding(.horizontal, 8).padding(.vertical, 3)
            .background(Color.hAccent, in: Capsule())
        }
        if let a = t.assignee, a.id != session.me?.member?.id, a.id != t.claimed_by?.id {
            Avatar(member: a)
        }
        if t.repeat != nil {
            Image(systemName: "repeat").font(.system(size: 12)).foregroundStyle(Color.hMutedFg)
        }
        if let p = t.todo_progress, p.total > 0 {
            HStack(spacing: 3) {
                Image(systemName: "checklist")
                Text("\(p.done)/\(p.total)").monospacedDigit()
            }
            .font(.system(size: 11, weight: .medium))
            .foregroundStyle(p.done == p.total ? Color.green : Color.hMutedFg)
        }
        DuePill(task: t)
    }
}
