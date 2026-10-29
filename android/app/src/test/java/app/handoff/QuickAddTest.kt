package app.handoff

import java.time.LocalDate
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

// Mirrors web/src/quickadd.test.ts — same grammar, same expectations.
class QuickAddTest {
    // Tuesday 13 Oct 2026.
    private val today = LocalDate.of(2026, 10, 13)
    private val members = listOf(Ref("m1", "Marta"), Ref("a1", "Legal bot"), Ref("m2", "Mar"))
    private val projects = listOf(Ref("p1", "Sales"), Ref("p2", "Q4 Launch"))
    private fun parse(s: String) = parseQuickAdd(s, members, projects, today)

    @Test
    fun workedExample() {
        val p = parse("Send contract to legal tomorrow @marta #Sales !1")
        assertEquals("Send contract to legal", p.title)
        assertEquals("2026-10-14", p.due)
        assertEquals("m1", p.assignee?.id)
        assertEquals("p1", p.project?.id)
        assertEquals(1, p.priority)
    }

    @Test
    fun multiWordNamesLongestWins() {
        assertEquals("a1", parse("Review NDA @legal-bot").assignee?.id)
        assertEquals("a1", parse("Review NDA @Legal bot today").assignee?.id)
        assertEquals("m2", parse("Call @mar").assignee?.id)
        assertEquals("m1", parse("Call @marta").assignee?.id)
        assertEquals("p2", parse("Plan #q4-launch").project?.id)
        assertNull(parse("Plan @marta-ish party").assignee)
        assertEquals("Plan @marta-ish party", parse("Plan @marta-ish party").title)
        assertNull(parse("Plan #Sales-ops review").project)
        assertEquals("p1", parse("Plan #Sales, then ship").project?.id)
    }

    @Test
    fun unknownWordsAndEmailsStay() {
        val p = parse("Email joan@openfort.xyz about #hiring and @nobody")
        assertEquals("Email joan@openfort.xyz about #hiring and @nobody", p.title)
        assertNull(p.assignee)
        assertNull(p.project)
    }

    @Test
    fun dates() {
        assertEquals("2026-10-13", parse("x today").due)
        assertEquals("2026-10-16", parse("x friday").due)
        assertEquals("2026-10-20", parse("x on tuesday").due)
        assertEquals("2026-10-19", parse("x next week").due)
        assertEquals("2026-10-16", parse("x in 3 days").due)
        assertEquals("2026-10-20", parse("x by Oct 20").due)
        assertEquals("2027-01-02", parse("x 2 jan").due)
        assertEquals("2026-11-30", parse("x 2026-11-30").due)
        assertNull(parse("x feb 30").due)
        assertEquals("2028-02-29", parse("x feb 29").due)
        assertEquals("2026-10-19", parse("Book the monday standup room").due)
        assertNull(parse("Fix todays bug").due)
        assertEquals("x", parse("x by Oct 20").title)
    }

    @Test
    fun priorityOnlyStandalone() {
        assertEquals(0, parse("Ship p0").priority)
        assertNull(parse("Upgrade p10 cluster").priority)
        assertNull(parse("Wow!1").priority)
    }

    @Test
    fun dueLabels() {
        assertEquals(DueLabel("Today", "today"), dueLabel("2026-10-13", today))
        assertEquals(DueLabel("Tomorrow", "soon"), dueLabel("2026-10-14", today))
        assertEquals(DueLabel("Yesterday", "overdue"), dueLabel("2026-10-12", today))
        assertEquals(DueLabel("Fri", "soon"), dueLabel("2026-10-16", today))
        assertEquals(DueLabel("Oct 1", "overdue"), dueLabel("2026-10-01", today))
        assertEquals(DueLabel("Nov 3", "later"), dueLabel("2026-11-03", today))
        assertEquals(DueLabel("Jan 2, 2027", "later"), dueLabel("2027-01-02", today))
    }

    @Test
    fun groupsByDue() {
        fun t(id: String, due: String? = null) = TaskItem(id = id, title = id, status = "open", due = due)
        val tasks = listOf(
            t("a", "2026-10-20"), t("b"), t("c", "2026-10-13"), t("d", "2026-10-01"),
            t("e", "2026-10-14"), t("f", "2026-10-12"), t("g"), t("h", "2026-10-13"),
        )
        assertEquals(
            listOf("overdue" to "df", "today" to "ch", "upcoming" to "ea", "none" to "bg"),
            groupByDue(tasks, today).map { it.key to it.tasks.joinToString("") { x -> x.id } },
        )
        assertEquals(listOf("none"), groupByDue(listOf(t("x")), today).map { it.key })
        assertEquals(emptyList<DueSection>(), groupByDue(emptyList(), today))
    }

    @Test
    fun repeats() {
        val p = parse("Water plants every day")
        assertEquals(Parsed(title = "Water plants", due = "2026-10-13", repeat = "daily"), p)
        assertEquals("weekdays", parse("Standup every weekday").repeat)
        assertEquals("weekly", parse("Send report every friday").repeat)
        assertEquals("2026-10-16", parse("Send report every friday").due)
        assertEquals("Send report", parse("Send report every friday").title)
        assertEquals("2026-10-13", parse("x every tuesday").due)
        assertEquals("biweekly", parse("1:1 every other week").repeat)
        assertEquals("monthly", parse("Pay rent monthly").repeat)
        assertEquals("yearly", parse("Renew domain yearly").repeat)
        assertEquals("monthly", parse("Invoice monthly from Oct 20").repeat)
        assertEquals("2026-10-20", parse("Invoice monthly from Oct 20").due)
        assertNull(parse("Plan monday meeting").repeat)
    }
}
