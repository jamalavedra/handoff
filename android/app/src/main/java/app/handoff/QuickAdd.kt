package app.handoff

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlinx.coroutines.launch

// Port of web/src/quickadd.ts — same syntax on every platform: "@member #project
// !priority, date words, repeats". Keep the rule order and boundary logic in sync.

data class Ref(val id: String, val name: String)

data class Parsed(
    val title: String,
    val due: String? = null,
    val assignee: Ref? = null,
    val project: Ref? = null,
    val priority: Int? = null,
    val repeat: String? = null,
)

private val WEEKDAYS = listOf("sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday")
private val MONTHS = listOf("jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec")
private const val MONTH = "(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)"
private const val LEAD = "(?:(?:on|by|due)\\s+)?"

private fun re(s: String) = Regex(s, RegexOption.IGNORE_CASE)

private fun addDays(d: LocalDate, n: Long): LocalDate = d.plusDays(n)

/** JS getDay(): Sunday=0 … Saturday=6. */
private fun dow(d: LocalDate): Int = d.dayOfWeek.value % 7

// JS `(8 - getDay()) % 7 || 7` and `(wd - getDay() + 7) % 7 || 7` — the next
// Monday / next named weekday, never today itself.
private fun nextMon(t: LocalDate): Long = ((8 - dow(t)) % 7).let { if (it == 0) 7L else it.toLong() }
private fun nextWeekday(t: LocalDate, wd: Int): Long = ((wd - dow(t) + 7) % 7).let { if (it == 0) 7L else it.toLong() }

private fun monthDay(month: String, day: String, today: LocalDate): LocalDate? {
    val m = MONTHS.indexOf(month.take(3).lowercase())
    val d = day.toIntOrNull() ?: return null
    if (m < 0 || d < 1 || d > 31) return null
    for (y in today.year..today.year + 8) {
        if (d <= java.time.YearMonth.of(y, m + 1).lengthOfMonth()) {
            val at = LocalDate.of(y, m + 1, d)
            if (!at.isBefore(today)) return at
        }
    }
    return null
}

private class DateRule(val re: Regex, val at: (MatchResult, LocalDate) -> LocalDate?)

private val dateRules = listOf(
    DateRule(re("$LEAD(\\d{4})-(\\d{2})-(\\d{2})")) { m, _ ->
        val y = m.groupValues[1].toInt(); val mo = m.groupValues[2].toInt(); val d = m.groupValues[3].toInt()
        if (mo in 1..12 && d <= java.time.YearMonth.of(y, mo).lengthOfMonth()) LocalDate.of(y, mo, d) else null
    },
    DateRule(re("$LEAD(?:today|tonight)")) { _, t -> t },
    DateRule(re("$LEAD(?:tomorrow|tmrw?)")) { _, t -> addDays(t, 1) },
    DateRule(re("(?:(?:by|due)\\s+)?next\\s+week")) { _, t -> addDays(t, nextMon(t)) },
    DateRule(re("in\\s+(\\d{1,3})\\s+(day|week)s?")) { m, t ->
        addDays(t, m.groupValues[1].toLong() * if (m.groupValues[2].lowercase() == "week") 7 else 1)
    },
    DateRule(re("$LEAD(?:next\\s+)?(${WEEKDAYS.joinToString("|")})")) { m, t ->
        val wd = WEEKDAYS.indexOf(m.groupValues[1].lowercase())
        addDays(t, nextWeekday(t, wd))
    },
    DateRule(re("$LEAD$MONTH\\s+(\\d{1,2})(?:st|nd|rd|th)?")) { m, t -> monthDay(m.groupValues[1], m.groupValues[2], t) },
    DateRule(re("$LEAD(\\d{1,2})(?:st|nd|rd|th)?\\s+$MONTH")) { m, t -> monthDay(m.groupValues[2], m.groupValues[1], t) },
)

private class RepeatRule(val re: Regex, val repeat: String, val start: (MatchResult, LocalDate) -> LocalDate)

// Repeats are read before dates, so "every friday" isn't taken as a one-off Friday.
private val repeatRules = listOf(
    RepeatRule(re("every\\s+day|daily"), "daily") { _, t -> t },
    RepeatRule(re("every\\s+weekday|weekdays"), "weekdays") { _, t ->
        addDays(t, when (dow(t)) { 6 -> 2; 0 -> 1; else -> 0 })
    },
    RepeatRule(re("every\\s+(${WEEKDAYS.joinToString("|")})"), "weekly") { m, t ->
        addDays(t, (WEEKDAYS.indexOf(m.groupValues[1].lowercase()) - dow(t) + 7) % 7L)
    },
    RepeatRule(re("every\\s+(?:other|2)\\s+weeks?|biweekly|fortnightly"), "biweekly") { _, t -> t },
    RepeatRule(re("every\\s+week|weekly"), "weekly") { _, t -> t },
    RepeatRule(re("every\\s+month|monthly"), "monthly") { _, t -> t },
    RepeatRule(re("every\\s+year|yearly|annually"), "yearly") { _, t -> t },
)

private fun norm(s: String) = s.lowercase().replace(Regex("[\\s_-]+"), "")
private val wordChar = Regex("[\\p{L}\\p{N}]")
private val sep = Regex("[\\s_-]")

// continuesWord: the text right after a candidate match continues the word
// (a letter or number after optional _ or -), so "@marta-ish" isn't "@marta".
private fun continuesWord(s: String): Boolean {
    var i = 0
    while (i < s.length && (s[i] == '_' || s[i] == '-')) i++
    return i < s.length && wordChar.matches(s[i].toString())
}

// matchRef finds the longest name in refs that the text right after a sigil starts with.
private fun matchRef(rest: String, refs: List<Ref>): Pair<Ref, Int>? {
    var best: Pair<Ref, Int>? = null
    for (ref in refs) {
        val target = norm(ref.name)
        if (target.isEmpty()) continue
        var seen = ""
        for (i in rest.indices) {
            val ch = rest[i]
            if (!sep.matches(ch.toString())) seen += ch.lowercaseChar()
            if (!target.startsWith(seen)) break
            if (seen == target && !continuesWord(rest.substring(i + 1))) {
                if (best == null || i + 1 > best!!.second) best = ref to (i + 1)
                break
            }
        }
    }
    return best
}

private fun takeRef(text: String, sigil: Char, refs: List<Ref>): Pair<String, Ref?> {
    for (i in text.indices) {
        if (text[i] != sigil || (i > 0 && !text[i - 1].isWhitespace())) continue
        val m = matchRef(text.substring(i + 1), refs) ?: continue
        return text.substring(0, i) + text.substring(i + 1 + m.second) to m.first
    }
    return text to null
}

private fun bounded(r: Regex) = Regex("(^|\\s)(${r.pattern})(?=$|[\\s.,;!?])", RegexOption.IGNORE_CASE)

private val priorityRe = Regex("(^|\\s)(?:!|p)([0-4])(?=$|[\\s.,;])", RegexOption.IGNORE_CASE)

// parseQuickAdd reads "Send contract to legal tomorrow @Marta #Sales !1" into a
// title plus a due date, assignee, project and priority. Unmatched @/# words stay in the title.
fun parseQuickAdd(text: String, members: List<Ref>, projects: List<Ref>, today: LocalDate = LocalDate.now()): Parsed {
    var t = text

    val (t1, assignee) = takeRef(t, '@', members)
    val (t2, project) = takeRef(t1, '#', projects)
    t = t2

    var priority: Int? = null
    priorityRe.find(t)?.let { m ->
        priority = m.groupValues[2].toInt()
        t = t.substring(0, m.range.first) + m.groupValues[1] + t.substring(m.range.last + 1)
    }

    var start: LocalDate? = null
    var repeat: String? = null
    for (rule in repeatRules) {
        val m = bounded(rule.re).find(t) ?: continue
        repeat = rule.repeat
        start = rule.start(rule.re.find(m.groupValues[2])!!, today)
        t = t.substring(0, m.range.first) + m.groupValues[1] + t.substring(m.range.last + 1)
        break
    }

    var due: String? = null
    for (rule in dateRules) {
        val m = bounded(rule.re).find(t) ?: continue
        val at = rule.at(rule.re.find(m.groupValues[2])!!, today) ?: continue
        due = at.toString()
        t = t.substring(0, m.range.first) + m.groupValues[1] + t.substring(m.range.last + 1)
        break
    }

    if (due == null && start != null) due = start.toString()
    return Parsed(title = Regex("\\s+").replace(t, " ").trim(), due = due, assignee = assignee,
        project = project, priority = priority, repeat = repeat)
}

data class DueLabel(val text: String, val tone: String)

private val MONTH_FMT = DateTimeFormatter.ofPattern("MMM d", Locale.US)
private val MONTH_YEAR_FMT = DateTimeFormatter.ofPattern("MMM d, yyyy", Locale.US)
private val WEEKDAY_FMT = DateTimeFormatter.ofPattern("EEE", Locale.US)
private val WEEKDAY_LONG = DateTimeFormatter.ofPattern("EEEE", Locale.US)

// dueLabel turns a YYYY-MM-DD due date into "Today", "Tomorrow", "Fri" or "Oct 14".
fun dueLabel(due: String, now: LocalDate = LocalDate.now()): DueLabel {
    val at = LocalDate.parse(due.take(10))
    val days = java.time.temporal.ChronoUnit.DAYS.between(now, at)
    val month = at.format(if (at.year != now.year) MONTH_YEAR_FMT else MONTH_FMT)
    return when {
        days == 0L -> DueLabel("Today", "today")
        days == 1L -> DueLabel("Tomorrow", "soon")
        days == -1L -> DueLabel("Yesterday", "overdue")
        days < 0 -> DueLabel(month, "overdue")
        days < 7 -> DueLabel(at.format(WEEKDAY_FMT), "soon")
        else -> DueLabel(month, "later")
    }
}

data class DueSection(val key: String, val title: String, val tasks: List<TaskItem>)

// groupByDue splits tasks into Overdue / Today / Upcoming / No date, dropping empty
// sections. Dated sections are ordered by due date; ties keep the input order.
fun groupByDue(tasks: List<TaskItem>, now: LocalDate = LocalDate.now()): List<DueSection> {
    val today = now.toString()
    val buckets = listOf("overdue" to "Overdue", "today" to "Today", "upcoming" to "Upcoming", "none" to "No date")
    val out = mutableListOf<DueSection>()
    for ((key, title) in buckets) {
        val inBucket = tasks.filter { t ->
            when {
                t.due == null -> key == "none"
                t.due < today -> key == "overdue"
                t.due == today -> key == "today"
                else -> key == "upcoming"
            }
        }
        if (inBucket.isNotEmpty()) {
            out += DueSection(key, title, if (key == "none") inBucket else inBucket.sortedBy { it.due })
        }
    }
    return out
}

// agentOnIt: an agent is working on an open task right now (its claim or pending triage).
fun agentOnIt(t: TaskItem): Boolean =
    t.status != "done" && t.status != "cancelled" && t.deleted_at == null &&
        (t.triage == "pending" || t.claimed_by?.kind == "agent")

// ── Quick add sheet ──────────────────────────────────────────────────────────

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QuickAddSheet(api: Api, onSignedOut: () -> Unit, onCreated: () -> Unit) {
    var text by remember { mutableStateOf("") }
    var members by remember { mutableStateOf<List<Ref>>(emptyList()) }
    var projects by remember { mutableStateOf<List<Ref>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) {
        callApi(onSignedOut, {}) {
            api.members().map { Ref(it.id, it.name) } to
                api.projects().map { Ref(it.id, it.name) }
        }?.let { (m, p) -> members = m; projects = p }
    }

    val parsed = parseQuickAdd(text, members, projects)

    Column(
        Modifier
            .fillMaxWidth()
            .navigationBarsPadding()
            .padding(horizontal = 16.dp)
            .padding(bottom = 24.dp),
    ) {
        OutlinedTextField(
            value = text,
            onValueChange = { text = it },
            placeholder = { Text("New task — try \"@name #list !0 friday\"") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
            shape = RoundedCornerShape(12.dp),
        )
        // Live preview chips: show what the syntax extracted.
        val chips = listOfNotNull(
            parsed.due?.let { "Due ${dueLabel(it).text}" },
            parsed.assignee?.let { "@${it.name}" },
            parsed.project?.let { "#${it.name}" },
            parsed.priority?.let { "p$it" },
            parsed.repeat,
        )
        if (chips.isNotEmpty()) {
            Row(Modifier.padding(top = 8.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                chips.forEach {
                    Text(
                        it, fontSize = 12.sp, color = hc.mutedFg,
                        modifier = Modifier
                            .background(hc.accent, RoundedCornerShape(50))
                            .padding(horizontal = 8.dp, vertical = 3.dp),
                    )
                }
            }
        }
        error?.let {
            Text(it, color = hc.destructive, fontSize = 13.sp, modifier = Modifier.padding(top = 8.dp))
        }
        Button(
            onClick = {
                scope.launch {
                    busy = true
                    callApi(onSignedOut, { error = it; busy = false }) {
                        api.createTasks(listOf(NewTask(
                            title = parsed.title,
                            due = parsed.due,
                            assignee = parsed.assignee?.id,
                            project = parsed.project?.id,
                            priority = parsed.priority,
                            repeat = parsed.repeat,
                        )))
                    }?.let { onCreated() }
                }
            },
            enabled = !busy && parsed.title.isNotBlank(),
            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
            colors = ButtonDefaults.buttonColors(containerColor = hc.primary, contentColor = hc.primaryFg),
        ) { Text("Add task") }
    }
}
