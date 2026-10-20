import SwiftUI

// Tokens from web/src/index.css.
private func themed(_ light: UIColor, _ dark: UIColor) -> Color {
    Color(UIColor { t in t.userInterfaceStyle == .dark ? dark : light })
}
extension Color {
    static let hBg = themed(.white, UIColor(red: 10 / 255, green: 10 / 255, blue: 10 / 255, alpha: 1))          // #0a0a0a
    static let hFg = themed(UIColor(red: 26 / 255, green: 26 / 255, blue: 26 / 255, alpha: 1), UIColor(red: 250 / 255, green: 250 / 255, blue: 250 / 255, alpha: 1))
    static let hCard = themed(.white, UIColor(red: 20 / 255, green: 20 / 255, blue: 20 / 255, alpha: 1))        // #141414
    static let hAccent = themed(.black.withAlphaComponent(0.06), .white.withAlphaComponent(0.08))
    static let hBorder = themed(.black.withAlphaComponent(0.08), .white.withAlphaComponent(0.10))
    static let hMutedFg = themed(UIColor(red: 115 / 255, green: 115 / 255, blue: 115 / 255, alpha: 1), UIColor(red: 163 / 255, green: 163 / 255, blue: 163 / 255, alpha: 1))
    static let hToday = themed(UIColor(red: 59 / 255, green: 130 / 255, blue: 246 / 255, alpha: 1), UIColor(red: 96 / 255, green: 165 / 255, blue: 250 / 255, alpha: 1))
    static let hUnseen = themed(UIColor(red: 249 / 255, green: 156 / 255, blue: 0, alpha: 1), UIColor(red: 252 / 255, green: 187 / 255, blue: 0, alpha: 1))
    static let hDestructive = themed(UIColor(red: 220 / 255, green: 38 / 255, blue: 38 / 255, alpha: 1), UIColor(red: 248 / 255, green: 113 / 255, blue: 113 / 255, alpha: 1))
    static let hViolet = themed(UIColor(red: 139 / 255, green: 92 / 255, blue: 246 / 255, alpha: 1), UIColor(red: 167 / 255, green: 139 / 255, blue: 250 / 255, alpha: 1))
    static let hAmber = Color(UIColor(red: 245 / 255, green: 158 / 255, blue: 11 / 255, alpha: 1))
    static let hAmberFg = themed(UIColor(red: 180 / 255, green: 83 / 255, blue: 9 / 255, alpha: 1), UIColor(red: 251 / 255, green: 191 / 255, blue: 36 / 255, alpha: 1))  // amber-700 / amber-400
}

// StatusCircle (components/app.tsx).
struct StatusCircle: View {
    var task: Task
    var size: CGFloat = 20
    var body: some View {
        switch task.status {
        case "done":
            Circle().fill(Color.hFg).frame(width: size, height: size)
                .overlay(Image(systemName: "checkmark").font(.system(size: size * 0.5, weight: .bold)).foregroundStyle(Color.hBg))
        case "cancelled":
            Circle().strokeBorder(Color.hMutedFg.opacity(0.5), lineWidth: 1.5).frame(width: size, height: size)
                .overlay(Image(systemName: "xmark").font(.system(size: size * 0.5, weight: .medium)).foregroundStyle(Color.hMutedFg))
        case _ where task.status == "in_progress" || task.claimed_by != nil:
            Circle().strokeBorder(Color.hAmber, lineWidth: 1.5).frame(width: size, height: size)
                .overlay(HalfDisc().fill(Color.hAmber).frame(width: size * 0.6, height: size * 0.6))
        default:
            let blocked = task.blocked_by?.isEmpty == false && !task.ready
            Circle().strokeBorder(style: StrokeStyle(lineWidth: 1.5, dash: blocked ? [3, 3] : []))
                .foregroundStyle(Color.hFg.opacity(0.4))
                .frame(width: size, height: size)
        }
    }
    // Left half of a disc — the web's half-filled "working" indicator.
    struct HalfDisc: Shape {
        func path(in r: CGRect) -> Path {
            var p = Path()
            p.addArc(center: CGPoint(x: r.midX, y: r.midY), radius: r.width / 2,
                     startAngle: .degrees(90), endAngle: .degrees(270), clockwise: false)
            p.closeSubpath()
            return p
        }
    }
}

// DuePill (components/app.tsx) — labels per quickadd.ts dueLabel.
struct DuePill: View {
    var task: Task
    private var label: String? {
        guard let due = task.due, let d = isoDay(due) else { return nil }
        let n = Calendar.current.dateComponents([.day], from: Calendar.current.startOfDay(for: Date()), to: d).day ?? 0
        if n == 0 { return "Today" }
        if n == 1 { return "Tomorrow" }
        if n == -1 { return "Yesterday" }
        if n < 0 { return DateFormatter.monthDay.string(from: d) }
        if n < 7 { return d.formatted(.dateTime.weekday(.abbreviated)) }
        var f = DateFormatter.monthDay.string(from: d)
        if !Calendar.current.isDate(d, equalTo: Date(), toGranularity: .year) { f += ", " + d.formatted(.dateTime.year()) }
        return f
    }
    var body: some View {
        if let label {
            let (bg, fg) = tone
            Text(label).font(.system(size: 11, weight: .medium)).monospacedDigit()
                .foregroundStyle(fg).padding(.horizontal, 8).padding(.vertical, 2)
                .background(bg, in: Capsule())
        }
    }
    private var tone: (Color, Color) {
        if task.closed { return (Color.hAccent, Color.hMutedFg.opacity(0.7)) }
        guard let due = task.due, let d = isoDay(due) else { return (Color.hAccent, Color.hMutedFg) }
        let n = Calendar.current.dateComponents([.day], from: Calendar.current.startOfDay(for: Date()), to: d).day ?? 0
        if n < 0 { return (Color.hDestructive.opacity(0.12), Color.hDestructive) }
        if n == 0 { return (Color.hToday.opacity(0.10), Color.hToday) }
        if n < 7 { return (Color.hAccent, Color.hFg.opacity(0.8)) }
        return (Color.hAccent, Color.hMutedFg)
    }
}

func isoDay(_ s: String) -> Date? {
    DateFormatter.cached.dateFormat = "yyyy-MM-dd"
    return DateFormatter.cached.date(from: String(s.prefix(10)))
}
extension DateFormatter {
    static let cached: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = TimeZone.current
        return f
    }()
    static let monthDay: DateFormatter = {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "MMM d"
        return f
    }()
}

// Avatar — size-6, violet for agents, initials for humans (components/app.tsx).
struct Avatar: View {
    var member: MemberRef?
    var size: CGFloat = 24
    var body: some View {
        if member?.kind == "agent" {
            Circle().fill(Color.hViolet.opacity(0.16)).frame(width: size, height: size)
                .overlay(Image(systemName: "cpu").font(.system(size: size * 0.5)).foregroundStyle(Color.hViolet))
        } else {
            Circle().fill(Color.hFg.opacity(0.1)).frame(width: size, height: size)
                .overlay(Text(initials(member?.name ?? "?")).font(.system(size: size * 0.42, weight: .semibold)).foregroundStyle(Color.hFg))
        }
    }
    private func initials(_ name: String) -> String {
        name.split(separator: " ").prefix(2).compactMap { $0.first }.map(String.init).joined().uppercased()
    }
}
