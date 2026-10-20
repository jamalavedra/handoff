import SwiftUI

// Task detail: meta, its todos (tap to tick), the comment thread, claim/close.
struct TaskDetailView: View {
    let id: String
    @EnvironmentObject var session: Session
    @State private var detail: TaskDetail?
    @State private var draft = ""
    @State private var error: String?
    @State private var busy = false

    private var task: Task? { detail?.task }

    var body: some View {
        List {
            if let t = task {
                Section {
                    if let body = t.body, !body.isEmpty {
                        Text(body).font(.callout)
                    }
                    meta("Status", statusText(t))
                    meta("Project", t.project)
                    if t.due != nil {
                        HStack {
                            Text("Due").foregroundStyle(Color.hMutedFg)
                            Spacer()
                            DuePill(task: t)
                        }
                        .font(.callout)
                    }
                    meta("Assignee", t.assignee?.name ?? "Unassigned")
                    if let c = t.claimed_by {
                        meta("Claimed by", "\(c.name)\(c.kind == "agent" ? " (agent)" : "")")
                    }
                    if let w = t.waiting_on {
                        meta("Waiting on", w.name)
                    }
                    if let l = t.labels, !l.isEmpty { meta("Labels", l.joined(separator: ", ")) }
                    if let b = t.blocked_by, !b.isEmpty { meta("Blocked by", b.joined(separator: ", ")) }
                }
                if let todos = t.todos, !todos.isEmpty {
                    Section("Steps") {
                        ForEach(todos) { td in
                            Button { _Concurrency.Task { await toggle(td) } } label: {
                                HStack {
                                    Image(systemName: td.done ? "checkmark.circle.fill" : "circle")
                                        .foregroundStyle(td.done ? Color.hFg : Color.hFg.opacity(0.4))
                                    VStack(alignment: .leading) {
                                        Text(td.text)
                                            .strikethrough(td.done)
                                            .foregroundStyle(td.done ? Color.hMutedFg : Color.hFg)
                                        if let r = td.result, !r.isEmpty {
                                            Text(r).font(.caption).foregroundStyle(Color.hMutedFg)
                                        }
                                    }
                                }
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                Section("Comments") {
                    ForEach(detail?.comments ?? []) { c in
                        VStack(alignment: .leading, spacing: 3) {
                            HStack {
                                Text(c.author?.name ?? "?")
                                    .font(.caption.weight(.semibold))
                                if c.author?.kind == "agent" {
                                    Image(systemName: "cpu").font(.caption2).foregroundStyle(Color.hViolet)
                                }
                                if let ob = c.on_behalf_of {
                                    Text("for \(ob.name)").font(.caption2).foregroundStyle(Color.hMutedFg.opacity(0.7))
                                }
                                Spacer()
                                Text(ago(c.created_at)).font(.caption2).foregroundStyle(Color.hMutedFg.opacity(0.7))
                            }
                            Text(c.body).font(.callout)
                        }
                        .padding(.vertical, 2)
                    }
                    if (detail?.comments ?? []).isEmpty {
                        Text("No comments yet.").foregroundStyle(Color.hMutedFg)
                    }
                }
            } else if let error {
                Text(error).foregroundStyle(Color.hDestructive)
            } else {
                ProgressView()
            }
        }
        .scrollContentBackground(.hidden)
        .background(Color.hBg)
        .navigationTitle(task?.title ?? "Task")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let t = task, !t.closed {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        if t.claimed_by == nil && t.ready {
                            Button("Claim") { _Concurrency.Task { await act { c in try await c.tool("claim_task", ["id": id]) } } }
                        }
                        if t.claimed_by != nil {
                            Button("Release") { _Concurrency.Task { await act { c in try await c.tool("release_task", ["id": id]) } } }
                        }
                        Button("Mark done") { _Concurrency.Task { await act { c in try await c.tool("close_task", ["id": id, "status": "done", "today": localDay()]) } } }
                        Button("Archive", role: .destructive) { _Concurrency.Task { await act { c in try await c.tool("close_task", ["id": id, "status": "archived", "today": localDay()]) } } }
                    } label: { Image(systemName: "ellipsis.circle") }
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            if task != nil && task?.closed == false {
                HStack {
                    TextField("Comment…", text: $draft, axis: .vertical)
                        .textFieldStyle(.roundedBorder)
                    Button { _Concurrency.Task { await send() } } label: { Image(systemName: "arrow.up.circle.fill").font(.title2) }
                        .disabled(draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || busy)
                }
                .padding(.horizontal)
                // Extra bottom so the composer clears the floating tab bar that
                // MainView overlays on every pushed view (~60pt bar + margins).
                .padding(.top, 8)
                .padding(.bottom, 84)
                .background(.bar)
            }
        }
        .task { await load() }
        .refreshable { await load() }
    }

    private func meta(_ k: String, _ v: String) -> some View {
        HStack {
            Text(k).foregroundStyle(Color.hMutedFg)
            Spacer()
            Text(v).multilineTextAlignment(.trailing).foregroundStyle(Color.hFg)
        }
        .font(.callout)
    }

    private func statusText(_ t: Task) -> String {
        switch t.status {
        case "done": "Done"
        case "cancelled": "Archived"
        case "in_progress": "In progress"
        default: t.ready ? "Ready" : "Open"
        }
    }

    private func load() async {
        guard let c = session.client else { return }
        do {
            detail = try await c.tool("get_task", ["id": id])
            error = nil
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func act(_ call: (Client) async throws -> TaskOut) async {
        guard let c = session.client else { return }
        busy = true
        defer { busy = false }
        do {
            _ = try await call(c)
            await load() // claim/close return the bare task; refetch to keep todos + comments
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func toggle(_ td: Todo) async {
        guard let c = session.client else { return }
        do {
            let out: TodosOut = try await c.tool("update_todo", ["id": td.id, "done": !td.done])
            detail?.task.todos = out.todos
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func send() async {
        let body = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !body.isEmpty, let c = session.client else { return }
        busy = true
        defer { busy = false }
        do {
            detail = try await c.tool("comment", ["id": id, "body": body])
            draft = ""
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch {
            self.error = error.localizedDescription
        }
    }
}

// Local YYYY-MM-DD — close_task counts a repeating task's next date from it (defaults to UTC otherwise).
func localDay() -> String {
    let f = DateFormatter()
    f.dateFormat = "yyyy-MM-dd"
    return f.string(from: .now)
}

func ago(_ iso: String) -> String {
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    let d = f.date(from: iso) ?? ISO8601DateFormatter().date(from: iso) ?? .distantPast
    let s = Date.now.timeIntervalSince(d)
    switch s {
    case ..<60: return "now"
    case ..<3600: return "\(Int(s / 60))m"
    case ..<86400: return "\(Int(s / 3600))h"
    default: return "\(Int(s / 86400))d"
    }
}
