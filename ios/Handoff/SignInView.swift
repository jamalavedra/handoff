import SwiftUI
import WebKit

// Sign in: pick the workspace host, then finish the web magic-link flow inside a
// WKWebView so its HTTPCookieStore captures `handoff_session`. Email links open
// in Safari, so pasting the emailed link here is the fallback path.
struct SignInView: View {
    @EnvironmentObject var session: Session
    @AppStorage("workspaceHost") private var host = ""
    @State private var busy = false
    @State private var error: String?
    @State private var pasteLink = ""
    @State private var webURL: URL?
    @State private var showCode = false
    @State private var code = ""

    // Accepts `acme.example.com`, `https://acme.example.com` or `acme.handoff.localhost:8080`.
    private var workspaceBase: URL? {
        var h = host.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if h.isEmpty { return nil }
        if !h.contains("://") {
            let local = h.contains("localhost") || h.hasPrefix("127.") || h.hasPrefix("[::1]")
            h = (local ? "http" : "https") + "://" + h
        }
        return URL(string: h)
    }

    var body: some View {
        NavigationStack {
            Group {
                if let c = session.client {
                    webSignIn(base: c.base)
                } else {
                    hostForm
                }
            }
            .navigationTitle("Handoff")
        }
    }

    private var hostForm: some View {
        VStack(spacing: 16) {
            Spacer()
            Text("Your shared task list")
                .foregroundStyle(.secondary)
            TextField("acme.yourhandoff.com", text: $host)
                .textFieldStyle(.roundedBorder)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
                .frame(maxWidth: 340)
            Text("The workspace address, e.g. acme.company.com — or acme.handoff.localhost:8080 for a local dev server.")
                .font(.caption)
                .foregroundStyle(.tertiary)
                .frame(maxWidth: 340)
            Button("Continue") { _Concurrency.Task { await connect() } }
                .buttonStyle(.borderedProminent)
                .disabled(workspaceBase == nil || busy)
            if busy { ProgressView() }
            if let error { Text(error).font(.callout).foregroundStyle(.red) }
            Spacer()
        }
        .padding()
    }

    private func connect() async {
        guard let base = workspaceBase else { return }
        busy = true
        error = nil
        defer { busy = false }
        do {
            let probe = Client(base: base)
            let m: Me = try await probe.get("api/me")
            if m.apex == true || m.org == nil {
                error = "That's the Handoff site, not a workspace — enter <slug>.<domain>."
                return
            }
            session.chooseWorkspace(base)
        } catch {
            self.error = "Can't reach that workspace: \(error.localizedDescription)"
        }
    }

    private func webSignIn(base: URL) -> some View {
        VStack(spacing: 0) {
            HStack {
                TextField("Paste your emailed sign-in link", text: $pasteLink)
                    .textFieldStyle(.roundedBorder)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)
                Button("Open") {
                    let s = pasteLink.trimmingCharacters(in: .whitespacesAndNewlines)
                    if let u = URL(string: s), let h = u.host,
                       h == base.host || h.hasSuffix(".\(base.host ?? "")") {
                        error = nil
                        webURL = u
                    } else {
                        error = "That link isn't for this workspace."
                    }
                }
                .disabled(URL(string: pasteLink.trimmingCharacters(in: .whitespacesAndNewlines)) == nil)
            }
            .padding(8)
            // "Pair a device" code from web Settings → redeemed for a session, no browser needed.
            if showCode {
                HStack {
                    TextField("XXXX-XXXX", text: $code)
                        .textFieldStyle(.roundedBorder)
                        .textInputAutocapitalization(.characters)
                        .autocorrectionDisabled()
                        .font(.body.monospaced())
                    Button("Redeem") { _Concurrency.Task { await redeem(base: base) } }
                        .disabled(code.trimmingCharacters(in: .whitespacesAndNewlines).count < 8 || busy)
                }
                .padding(.horizontal, 8).padding(.bottom, 8)
            } else {
                Button("Have a code?") { showCode = true }
                    .font(.caption)
                    .padding(.bottom, 6)
            }
            if let error { Text(error).font(.caption).foregroundStyle(.red).padding(.horizontal, 8) }
            WebView(url: webURL ?? base.appendingPathComponent("login"), host: base.host ?? "") { token in
                _Concurrency.Task { await session.didSignIn(token: token) }
            }
        }
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Back") { session.client = nil }
            }
        }
    }

    // POST /api/app/redeem — swaps a "Pair a device" code for a session token.
    private func redeem(base: URL) async {
        busy = true
        error = nil
        defer { busy = false }
        do {
            let out: RedeemOut = try await Client(base: base)
                .request("POST", "api/app/redeem", ["code": code.trimmingCharacters(in: .whitespacesAndNewlines)])
            await session.didSignIn(token: out.token)
        } catch let e as APIError {
            switch e.status {
            case 400: error = "That code is invalid, expired, or already used."
            case 404: error = "This server doesn't know pairing codes yet — use the link instead."
            case 429: error = "Too many tries — wait a bit and try again."
            default: error = e.message
            }
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct WebView: UIViewRepresentable {
    let url: URL
    let host: String
    var onCookie: (String) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(host: host, onCookie: onCookie) }

    func makeUIView(context: Context) -> WKWebView {
        let wv = WKWebView()
        wv.navigationDelegate = context.coordinator
        context.coordinator.load(wv, url)
        return wv
    }

    func updateUIView(_ wv: WKWebView, context: Context) {
        context.coordinator.load(wv, url)
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        let host: String
        let onCookie: (String) -> Void
        private var last: URL?

        init(host: String, onCookie: @escaping (String) -> Void) {
            self.host = host
            self.onCookie = onCookie
        }

        func load(_ wv: WKWebView, _ url: URL) {
            guard url != last else { return }
            last = url
            wv.load(URLRequest(url: url))
        }

        func webView(_ wv: WKWebView, didFinish _: WKNavigation!) { grab(wv) }

        private func grab(_ wv: WKWebView) {
            wv.configuration.websiteDataStore.httpCookieStore.getAllCookies { cookies in
                let bare = self.host.split(separator: ":").first.map(String.init) ?? self.host
                guard let c = cookies.first(where: {
                    $0.name == "handoff_session" && ($0.domain == bare || $0.domain == ".\(bare)")
                }) else { return }
                DispatchQueue.main.async { self.onCookie(c.value) }
            }
        }
    }
}
