import SwiftUI
import WebKit

@main
struct HandoffApp: App {
    @StateObject private var session = Session()
    var body: some Scene {
        WindowGroup {
            RootView().environmentObject(session)
        }
    }
}

@MainActor
final class Session: ObservableObject {
    // client exists once a workspace host is picked; signedIn once the cookie is verified.
    @Published var client: Client?
    @Published var me: Me?
    @Published var signedIn = false

    init() { restore() }

    private func restore() {
        guard let data = Keychain.load(),
              let saved = try? JSONDecoder().decode(SavedSession.self, from: data),
              let base = URL(string: saved.base) else { return }
        client = Client(base: base, cookie: saved.token)
        _Concurrency.Task { await verify() }
    }

    func chooseWorkspace(_ base: URL) {
        client = Client(base: base)
    }

    // The WebView captured handoff_session for this host.
    func didSignIn(token: String) async {
        guard let c = client else { return }
        client = Client(base: c.base, cookie: token)
        await verify()
    }

    func verify() async {
        guard let c = client else { return }
        do {
            let m: Me = try await c.get("api/me")
            me = m
            if m.member != nil {
                signedIn = true
                let saved = SavedSession(base: c.base.absoluteString, token: c.cookie)
                Keychain.save((try? JSONEncoder().encode(saved)) ?? Data())
            } else {
                // Valid response but no member: the saved cookie is dead — drop it.
                signOut(serverCall: false)
            }
        } catch let e as APIError where e.unauthorized {
            signOut(serverCall: false)
        } catch {}
    }

    func expire() { signOut(serverCall: false) }

    func signOut(serverCall: Bool = true) {
        if serverCall, let c = client {
            _Concurrency.Task { try? await c.post("api/logout") }
        }
        Keychain.clear()
        client = nil
        me = nil
        signedIn = false
        // Drop the web sign-in cookie too so the next login starts clean.
        WKWebsiteDataStore.default().removeData(ofTypes: [WKWebsiteDataTypeCookies], modifiedSince: .distantPast) {}
    }
}

struct RootView: View {
    @EnvironmentObject var session: Session
    var body: some View {
        if session.signedIn {
            MainView()
        } else {
            SignInView()
        }
    }
}
