import Foundation

// Port of web/src/quickadd.ts — same quick-add grammar on every platform.
struct QuickRef: Identifiable, Hashable {
    var id: String
    var name: String
}

struct QuickParsed {
    var title = ""
    var due: String?
    var assignee: QuickRef?
    var project: QuickRef?
    var priority: Int?
    var `repeat`: String?
}

let repeatLabel: [String: String] = [
    "daily": "Every day", "weekdays": "Every weekday", "weekly": "Every week",
    "biweekly": "Every 2 weeks", "monthly": "Every month", "yearly": "Every year",
]

func scheduleLabel(_ repeatKind: String, due: String?) -> String {
    guard let due, let d = isoDay(due), repeatKind != "daily", repeatKind != "weekdays" else { return repeatLabel[repeatKind] ?? repeatKind }
    let comps = Calendar.current.dateComponents([.day, .month, .weekday], from: d)
    let weekday = d.formatted(.dateTime.weekday(.wide))
    switch repeatKind {
    case "weekly": return "Every \(weekday)"
    case "biweekly": return "Every other \(weekday)"
    case "monthly": return "Every month on the \(ordinal(comps.day ?? 0))"
    default:
        return "Every year on \(DateFormatter.monthDay.string(from: d))"
    }
}

func ordinal(_ n: Int) -> String {
    let s = (11...13).contains(n % 100) ? "th" : (n % 10 == 1 ? "st" : n % 10 == 2 ? "nd" : n % 10 == 3 ? "rd" : "th")
    return "\(n)\(s)"
}

private let WEEKDAYS = ["sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"]
private let MONTHS = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"]
private let MONTH = "(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)"
private let LEAD = "(?:(?:on|by|due)\\s+)?"

func isoDate(_ d: Date) -> String {
    DateFormatter.cached.dateFormat = "yyyy-MM-dd"
    return DateFormatter.cached.string(from: d)
}
private func addDays(_ d: Date, _ n: Int) -> Date { Calendar.current.date(byAdding: .day, value: n, to: d)! }
private func dayOf(_ d: Date, _ c: Calendar.Component) -> Int { Calendar.current.component(c, from: d) }
private func dayStart(_ d: Date) -> Date { Calendar.current.startOfDay(for: d) }
private func monthDay(_ month: String, _ day: Int, _ today: Date) -> Date? {
    guard let m = MONTHS.firstIndex(of: String(month.prefix(3)).lowercased()), (1...31).contains(day) else { return nil }
    for y in dayOf(today, .year)...(dayOf(today, .year) + 8) {
        var c = DateComponents()
        c.year = y; c.month = m + 1; c.day = day
        if let at = Calendar.current.date(from: c), dayOf(at, .month) == m + 1, at >= today { return at }
    }
    return nil
}
private func mkDay(_ y: Int, _ m: Int, _ d: Int) -> Date? {
    var c = DateComponents(); c.year = y; c.month = m; c.day = d
    guard let at = Calendar.current.date(from: c), dayOf(at, .month) == m else { return nil }
    return at
}

private struct DateRule {
    var re: NSRegularExpression
    var at: ([String], Date) -> Date?
}
private struct RepeatRule {
    var re: NSRegularExpression
    var kind: String
    var start: ([String], Date) -> Date
}

private func rx(_ pattern: String) -> NSRegularExpression {
    try! NSRegularExpression(pattern: pattern, options: [.caseInsensitive])
}

private let dateRules: [DateRule] = [
    .init(re: rx("\(LEAD)(\\d{4})-(\\d{2})-(\\d{2})"), at: { m, _ in
        mkDay(Int(m[1]) ?? 0, Int(m[2]) ?? 0, Int(m[3]) ?? 0)
    }),
    .init(re: rx("\(LEAD)(?:today|tonight)"), at: { _, t in t }),
    .init(re: rx("\(LEAD)(?:tomorrow|tmrw?)"), at: { _, t in addDays(t, 1) }),
    .init(re: rx("(?:(?:by|due)\\s+)?next\\s+week"), at: { _, t in
        let g = dayOf(t, .weekday) - 1 // Swift weekday is 1-based, JS getDay() 0-based
        let n = (8 - g) % 7
        return addDays(t, n == 0 ? 7 : n)
    }),
    .init(re: rx("in\\s+(\\d{1,3})\\s+(day|week)s?"), at: { m, t in
        addDays(t, (Int(m[1]) ?? 0) * (m[2].lowercased() == "week" ? 7 : 1))
    }),
    .init(re: rx("\(LEAD)(?:next\\s+)?(\(WEEKDAYS.joined(separator: "|")))"), at: { m, t in
        let wd = WEEKDAYS.firstIndex(of: m[1].lowercased()) ?? 0
        let d = (wd - (dayOf(t, .weekday) - 1) + 7) % 7
        return addDays(t, d == 0 ? 7 : d)
    }),
    .init(re: rx("\(LEAD)\(MONTH)\\s+(\\d{1,2})(?:st|nd|rd|th)?"), at: { m, t in monthDay(m[1], Int(m[2]) ?? 0, t) }),
    .init(re: rx("\(LEAD)(\\d{1,2})(?:st|nd|rd|th)?\\s+\(MONTH)"), at: { m, t in monthDay(m[2], Int(m[1]) ?? 0, t) }),
]

private let repeatRules: [RepeatRule] = [
    .init(re: rx("every\\s+day|daily"), kind: "daily", start: { _, t in t }),
    .init(re: rx("every\\s+weekday|weekdays"), kind: "weekdays", start: { _, t in addDays(t, dayOf(t, .weekday) == 7 ? 2 : dayOf(t, .weekday) == 1 ? 1 : 0) }),
    .init(re: rx("every\\s+(\(WEEKDAYS.joined(separator: "|")))"), kind: "weekly", start: { m, t in
        addDays(t, ((WEEKDAYS.firstIndex(of: m[1].lowercased()) ?? 0) - (dayOf(t, .weekday) - 1) + 7) % 7)
    }),
    .init(re: rx("every\\s+(?:other|2)\\s+weeks?|biweekly|fortnightly"), kind: "biweekly", start: { _, t in t }),
    .init(re: rx("every\\s+week|weekly"), kind: "weekly", start: { _, t in t }),
    .init(re: rx("every\\s+month|monthly"), kind: "monthly", start: { _, t in t }),
    .init(re: rx("every\\s+year|yearly|annually"), kind: "yearly", start: { _, t in t }),
]

private func norm(_ s: String) -> String {
    s.lowercased().replacingOccurrences(of: "[\\s_-]+", with: "", options: .regularExpression)
}
private func continuesWord(_ s: String) -> Bool {
    s.range(of: "^[_-]*[\\p{L}\\p{N}]", options: .regularExpression) != nil
}

private func matchRef(_ rest: Substring, _ refs: [QuickRef]) -> (ref: QuickRef, len: Int)? {
    var best: (QuickRef, Int)?
    for ref in refs {
        let target = norm(ref.name)
        if target.isEmpty { continue }
        var seen = ""
        let chars = Array(rest)
        for i in 0..<chars.count {
            let ch = chars[i]
            if !ch.isWhitespace && ch != "_" && ch != "-" { seen.append(Character(ch.lowercased())) }
            if !target.hasPrefix(seen) { break }
            if seen == target && !continuesWord(String(chars[(i + 1)...])) {
                if best == nil || i + 1 > best!.1 { best = (ref, i + 1) }
                break
            }
        }
    }
    return best
}

private func takeRef(_ text: String, _ sigil: Character, _ refs: [QuickRef]) -> (String, QuickRef?) {
    let chars = Array(text)
    for i in 0..<chars.count where chars[i] == sigil {
        if i > 0 && !chars[i - 1].isWhitespace { continue }
        if let m = matchRef(text[text.index(text.startIndex, offsetBy: i + 1)...], refs) {
            return (String(chars[0..<i]) + String(chars[(i + 1 + m.len)...]), m.ref)
        }
    }
    return (text, nil)
}

// bounded: match only when the pattern is preceded by start/space and followed by end/space/punct.
private struct BoundedMatch {
    var range: Range<String.Index>
    var innerRange: Range<String.Index>
    var groups: [String]
}
private func bounded(_ re: NSRegularExpression, in text: String) -> BoundedMatch? {
    let pattern = "(^|\\s)(\(re.pattern))(?=$|[\\s.,;!?])"
    guard let b = try? NSRegularExpression(pattern: pattern, options: [.caseInsensitive]) else { return nil }
    guard let m = b.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) else { return nil }
    var groups: [String] = []
    for i in 0..<m.numberOfRanges {
        let r = m.range(at: i)
        groups.append(r.location != NSNotFound && r.length > 0 ? String(text[Range(r, in: text)!]) : "")
    }
    return BoundedMatch(range: Range(m.range, in: text)!, innerRange: Range(m.range(at: 2), in: text)!, groups: groups)
}

private func innerGroups(_ re: NSRegularExpression, of inner: String) -> [String] {
    guard let m = re.firstMatch(in: inner, range: NSRange(inner.startIndex..., in: inner)) else { return [] }
    return (0..<m.numberOfRanges).map { i in
        let r = m.range(at: i)
        return r.location != NSNotFound && r.length > 0 ? String(inner[Range(r, in: inner)!]) : ""
    }
}

// parseQuickAdd: "Send contract to legal tomorrow @Marta #Sales !1" → title + due + assignee + project + priority.
func parseQuickAdd(_ text: String, members: [QuickRef], projects: [QuickRef], now: Date = Date()) -> QuickParsed {
    let today = dayStart(now)
    var out = QuickParsed()
    var t = text
    (t, out.assignee) = takeRef(t, "@", members)
    (t, out.project) = takeRef(t, "#", projects)

    // JS splices keep group 1 (the leading whitespace) when cutting a match.
    if let pr = try? NSRegularExpression(pattern: "(^|\\s)(?:!|p)([0-4])(?=$|[\\s.,;])", options: [.caseInsensitive]),
       let m = pr.firstMatch(in: t, range: NSRange(t.startIndex..., in: t)) {
        out.priority = Int(String(t[Range(m.range(at: 2), in: t)!]))
        let r = Range(m.range, in: t)!
        t = String(t[..<r.lowerBound]) + String(t[Range(m.range(at: 1), in: t)!]) + t[r.upperBound...]
    }

    var start: Date?
    for rule in repeatRules {
        guard let m = bounded(rule.re, in: t) else { continue }
        out.repeat = rule.kind
        start = rule.start(innerGroups(rule.re, of: String(t[m.innerRange])), today)
        let r = m.range
        t = String(t[..<r.lowerBound]) + m.groups[1] + t[r.upperBound...]
        break
    }

    for rule in dateRules {
        guard let m = bounded(rule.re, in: t),
              let at = rule.at(innerGroups(rule.re, of: String(t[m.innerRange])), today) else { continue }
        out.due = isoDate(at)
        let r = m.range
        t = String(t[..<r.lowerBound]) + m.groups[1] + t[r.upperBound...]
        break
    }

    if out.due == nil, let start { out.due = isoDate(start) }
    out.title = t.replacingOccurrences(of: "\\s+", with: " ", options: .regularExpression).trimmingCharacters(in: .whitespaces)
    return out
}

// groupByDue: Overdue / Today / Upcoming / No date, dated sections sorted by due.
struct DueSection: Identifiable {
    var key: String
    var title: String
    var tasks: [Task]
    var id: String { key }
}
func groupByDue(_ tasks: [Task], now: Date = Date()) -> [DueSection] {
    let today = isoDate(now)
    var buckets: [(key: String, title: String, tasks: [Task])] = [
        ("overdue", "Overdue", []), ("today", "Today", []), ("upcoming", "Upcoming", []), ("none", "No date", []),
    ]
    for t in tasks {
        let i = t.due == nil ? 3 : (t.due! < today ? 0 : t.due! == today ? 1 : 2)
        buckets[i].tasks.append(t)
    }
    for i in 0..<3 {
        buckets[i].tasks.sort { ($0.due ?? "") < ($1.due ?? "") }
    }
    return buckets.filter { !$0.tasks.isEmpty }.map { DueSection(key: $0.key, title: $0.title, tasks: $0.tasks) }
}
