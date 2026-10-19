import SwiftUI

// The task list: open work by default, plus the Ready view the agents pick from.
struct TasksView: View {
    @EnvironmentObject var session: Session
    @State private var scope = Scope.open
    @State private var tasks: [Task] = []
    @State private var error: String?
    @State private var showNew = false

    enum Scope: String, CaseIterable, Identifiable {
        case open = "Open", ready = "Ready", mine = "Mine"
        var id: Self { self }
    }

    var body: some View {
        List {
            if let error { Text(error).foregroundStyle(.red) }
            ForEach(tasks) { t in
                NavigationLink(value: t.id) { TaskRow(t) }
            }
            if tasks.isEmpty && error == nil {
                Text("Nothing here.").foregroundStyle(.secondary)
            }
        }
        .navigationTitle(session.me?.org?.name ?? "Tasks")
        .navigationDestination(for: String.self) { TaskDetailView(id: $0) }
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Menu {
                    if let m = session.me?.member { Text(m.name) }
                    Button("Sign out", role: .destructive) { session.signOut() }
                } label: {
                    Image(systemName: "person.circle")
                }
            }
            ToolbarItem(placement: .principal) {
                Picker("Scope", selection: $scope) {
                    ForEach(Scope.allCases) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                .frame(width: 240)
            }
            ToolbarItem(placement: .primaryAction) {
                Button { showNew = true } label: { Image(systemName: "plus") }
            }
        }
        .sheet(isPresented: $showNew) {
            NavigationStack { NewTaskView { _Concurrency.Task { await load() } } }
        }
        .refreshable { await load() }
        .task { await load() }
        .task(id: scope) { await load() }
    }

    func load() async {
        guard let c = session.client else { return }
        do {
            if scope == .ready {
                let out: TasksOut = try await c.tool("list_ready", ["limit": 100])
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
                tasks = all
            }
            error = nil
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct TaskRow: View {
    let t: Task
    init(_ t: Task) { self.t = t }

    var body: some View {
        HStack(alignment: .center, spacing: 10) {
            VStack(alignment: .leading, spacing: 3) {
                Text(t.title)
                    .lineLimit(2)
                    .strikethrough(t.closed)
                    .foregroundStyle(t.closed ? .secondary : .primary)
                HStack(spacing: 0) {
                    Text(t.project)
                    if let a = t.assignee { Text(" · \(a.name)") }
                    if let d = t.due {
                        Text(" · \(dueLabel(d))")
                            .foregroundStyle(overdue(d) ? .red : .secondary)
                    }
                    if let p = t.todo_progress { Text(" · \(p.done)/\(p.total)") }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                if let note = t.agent_note, !note.isEmpty {
                    Text(note.components(separatedBy: "\n").first ?? note)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                }
            }
            if t.agentWorking {
                Circle().fill(.purple).frame(width: 8, height: 8)
            } else if t.claimed_by != nil {
                Circle().fill(.orange).frame(width: 8, height: 8)
            }
            if t.waiting_on != nil {
                Image(systemName: "questionmark.circle").foregroundStyle(.orange)
            }
        }
    }
}

// due is "YYYY-MM-DD".
func dueLabel(_ due: String) -> String {
    let f = DateFormatter()
    f.dateFormat = "yyyy-MM-dd"
    f.timeZone = .current
    guard let d = f.date(from: due) else { return due }
    let days = Calendar.current.dateComponents([.day], from: Calendar.current.startOfDay(for: .now), to: d).day ?? 0
    switch days {
    case ..<0: return due
    case 0: return "Today"
    case 1: return "Tomorrow"
    default:
        f.dateFormat = "MMM d"
        return f.string(from: d)
    }
}

func overdue(_ due: String) -> Bool {
    let f = DateFormatter()
    f.dateFormat = "yyyy-MM-dd"
    guard let d = f.date(from: due) else { return false }
    return d < Calendar.current.startOfDay(for: .now)
}
