import Foundation

struct APIError: LocalizedError {
    var status: Int
    var message: String
    var errorDescription: String? { message }
    var unauthorized: Bool { status == 401 } // 403 is a permission error, not an expired session
}

// JSON mirrors web/src/lib.ts: the server speaks snake_case.
struct MemberRef: Codable { var id, name, kind: String }
struct Todo: Codable, Identifiable { var id, text: String; var done: Bool; var result: String?; var position: Int }
struct TodoProgress: Codable { var done, total: Int }

struct Task: Codable, Identifiable {
    var id: String
    var title: String
    var body: String?
    var status: String
    var priority: Int
    var project: String
    var assignee: MemberRef?
    var due: String?
    var labels: [String]?
    var claimed_by: MemberRef?
    var claim_expires_at: String?
    var waiting_on: MemberRef?
    var agent_note: String?
    var created_at: String?
    var blocked_by: [String]?
    var ready: Bool
    var todos: [Todo]?
    var todo_progress: TodoProgress?

    var closed: Bool { status == "done" || status == "cancelled" }
    var agentWorking: Bool { !closed && claimed_by?.kind == "agent" }
}

struct Project: Codable, Identifiable { var id, name: String; var emoji: String?; var open_tasks: Int }
struct Comment: Codable, Identifiable {
    var id: String
    var author: MemberRef?
    var on_behalf_of: MemberRef?
    var body: String
    var created_at: String
}
struct TaskDetail: Codable { var task: Task; var comments: [Comment] }
struct TasksOut: Codable { var tasks: [Task] }
struct TaskOut: Codable { var task: Task }
struct TodosOut: Codable { var todos: [Todo] }
struct ProjectsOut: Codable { var projects: [Project] }
struct CreateTasksOut: Codable { var tasks: [Task]; var created: Int }
struct Org: Codable { var name, slug: String }
struct Member: Codable { var id, name: String; var email: String?; var role: String }
struct Me: Codable { var apex: Bool?; var org: Org?; var member: Member? }

// Thin wrapper over the JSON API the web app uses: session cookie + X-Handoff CSRF header.
struct Client {
    var base: URL
    var cookie = ""

    @discardableResult
    func request<T: Decodable>(_ method: String, _ path: String, _ body: [String: Any]? = nil) async throws -> T {
        var req = URLRequest(url: base.appendingPathComponent(path))
        req.httpMethod = method
        req.setValue("1", forHTTPHeaderField: "X-Handoff")
        if !cookie.isEmpty {
            req.setValue("handoff_session=\(cookie)", forHTTPHeaderField: "Cookie")
        }
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let (data, res) = try await URLSession.shared.data(for: req)
        let code = (res as? HTTPURLResponse)?.statusCode ?? -1
        guard (200...299).contains(code) else {
            let msg = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
            throw APIError(status: code, message: msg?["error"] as? String ?? HTTPURLResponse.localizedString(forStatusCode: code))
        }
        return try JSONDecoder().decode(T.self, from: data)
    }

    func get<T: Decodable>(_ path: String) async throws -> T { try await request("GET", path) }
    func post(_ path: String, _ body: [String: Any]? = nil) async throws {
        let _: [String: Bool] = try await request("POST", path, body)
    }
    func tool<T: Decodable>(_ name: String, _ input: [String: Any] = [:]) async throws -> T {
        try await request("POST", "api/tools/\(name)", input)
    }
}

struct SavedSession: Codable { var base: String; var token: String }

enum Keychain {
    private static let query: [String: Any] = [
        kSecClass as String: kSecClassGenericPassword,
        kSecAttrService as String: "app.handoff.ios",
        kSecAttrAccount as String: "session",
        kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock,
    ]
    static func save(_ data: Data) {
        SecItemDelete(query as CFDictionary)
        var q = query
        q[kSecValueData as String] = data
        SecItemAdd(q as CFDictionary, nil)
    }
    static func load() -> Data? {
        var q = query
        q[kSecReturnData as String] = true
        var out: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess else { return nil }
        return out as? Data
    }
    static func clear() { SecItemDelete(query as CFDictionary) }
}
