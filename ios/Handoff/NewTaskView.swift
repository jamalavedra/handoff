import SwiftUI

// New task: title, optional project and due date. Deliberately simple — the web
// app's quick-add parser stays on the web.
struct NewTaskView: View {
    @EnvironmentObject var session: Session
    var onCreated: () -> Void
    @Environment(\.dismiss) private var dismiss

    @State private var title = ""
    @State private var project = ""
    @State private var hasDue = false
    @State private var due = Date.now
    @State private var projects: [Project] = []
    @State private var busy = false
    @State private var error: String?
    // One id per sheet so a retry after a lost response is a no-op server-side.
    @State private var extId = "ios-\(UUID().uuidString)"

    var body: some View {
        Form {
            Section {
                TextField("What needs doing?", text: $title)
            }
            Section {
                Picker("Project", selection: $project) {
                    Text("Inbox").tag("")
                    ForEach(projects.filter { $0.name != "Inbox" }) { p in
                        Text("\(p.emoji ?? "")\(p.emoji == nil ? "" : " ")\(p.name)").tag(p.name)
                    }
                }
                Toggle("Due date", isOn: $hasDue)
                if hasDue {
                    DatePicker("Due", selection: $due, displayedComponents: .date)
                }
            }
            if let error { Text(error).foregroundStyle(.red) }
        }
        .navigationTitle("New task")
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            ToolbarItem(placement: .confirmationAction) {
                Button("Create") { _Concurrency.Task { await create() } }
                    .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty || busy)
            }
        }
        .task {
            guard let c = session.client else { return }
            if let out: ProjectsOut = try? await c.tool("list_projects") {
                projects = out.projects
            }
        }
    }

    private func create() async {
        guard let c = session.client else { return }
        busy = true
        defer { busy = false }
        var t: [String: Any] = ["title": title.trimmingCharacters(in: .whitespaces), "external_id": extId]
        if !project.isEmpty { t["project"] = project }
        if hasDue {
            let f = DateFormatter()
            f.dateFormat = "yyyy-MM-dd"
            t["due"] = f.string(from: due)
        }
        do {
            let _: CreateTasksOut = try await c.tool("create_tasks", ["tasks": [t]])
            onCreated()
            dismiss()
        } catch let e as APIError {
            if e.unauthorized { session.expire() } else { error = e.message }
        } catch {
            self.error = error.localizedDescription
        }
    }
}
