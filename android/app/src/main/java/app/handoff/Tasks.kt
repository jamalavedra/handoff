package app.handoff

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.AlternateEmail
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.CheckBox
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Flag
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.MoreHoriz
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.PersonAdd
import androidx.compose.material.icons.filled.PlaylistAddCheck
import androidx.compose.material.icons.filled.Repeat
import androidx.compose.material.icons.filled.Shield
import androidx.compose.material.icons.filled.SmartToy
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material3.Badge
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// ── StatusCircle / DuePill / TaskRow: native ports of web/src/components/app.tsx
// and pages/tasks.tsx TaskRow. Same states, same colors.

@Composable
fun StatusCircle(t: TaskItem, onToggle: (() -> Unit)? = null) {
    val working = t.status == "in_progress" || t.claimed_by != null
    val mod = Modifier.size(20.dp).let { m -> if (onToggle != null) m.clickable(onClick = onToggle) else m }
    when {
        t.status == "done" -> Box(
            mod.background(hc.primary, CircleShape),
            contentAlignment = Alignment.Center,
        ) { Icon(Icons.Default.Check, null, tint = hc.primaryFg, modifier = Modifier.size(14.dp)) }
        t.status == "cancelled" -> Box(
            mod.border(1.5.dp, hc.input, CircleShape),
            contentAlignment = Alignment.Center,
        ) { Icon(Icons.Default.Close, null, tint = hc.mutedFg.copy(alpha = .7f), modifier = Modifier.size(12.dp)) }
        working -> {
            val amber = hc.amber
            Canvas(mod) {
                val r = size.minDimension / 2 - 1.5.dp.toPx() / 2
                drawCircle(amber, radius = r, style = Stroke(width = 1.5.dp.toPx()))
                drawArc(amber, startAngle = 90f, sweepAngle = 180f, useCenter = true)
            }
        }
        else -> {
            val blocked = !t.ready && t.blocked_by.isNotEmpty()
            val strokeColor = if (blocked) hc.input else hc.fg.copy(alpha = .4f)
            Canvas(mod) {
                val stroke = Stroke(
                    width = 1.5.dp.toPx(),
                    pathEffect = if (blocked) PathEffect.dashPathEffect(floatArrayOf(4f, 3f)) else null,
                )
                drawCircle(strokeColor, radius = size.minDimension / 2 - 1f, style = stroke)
            }
        }
    }
}

@Composable
private fun Pill(text: String, bg: Color, fg: Color, icon: ImageVector? = null, iconTint: Color = fg) {
    Row(
        modifier = Modifier.clip(RoundedCornerShape(50)).background(bg).padding(horizontal = 8.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        icon?.let { Icon(it, null, tint = iconTint, modifier = Modifier.size(12.dp)) }
        Text(text, fontSize = 12.sp, fontWeight = FontWeight.Medium, color = fg, maxLines = 1)
    }
}

@Composable
fun DuePill(due: String, done: Boolean) {
    val l = dueLabel(due)
    when {
        done -> Pill(l.text, hc.accent, hc.mutedFg.copy(alpha = .7f))
        l.tone == "overdue" -> Pill(l.text, Color(0xFFFEF2F2), Color(0xFFB91C1C))
        l.tone == "today" -> Pill(l.text, hc.today.copy(alpha = .10f), hc.today)
        l.tone == "soon" -> Pill(l.text, hc.accent, hc.fg.copy(alpha = .8f))
        else -> Pill(l.text, hc.accent, hc.mutedFg)
    }
}

@Composable
fun Avatar(m: Member, size: Int = 24) {
    val initials = m.name.split(Regex("\\s+")).mapNotNull { it.firstOrNull() }.joinToString("").take(2).uppercase()
    Box(
        Modifier.size(size.dp).clip(CircleShape)
            .background(if (m.kind == "agent") hc.violet.copy(alpha = .18f) else hc.fg.copy(alpha = .10f)),
        contentAlignment = Alignment.Center,
    ) {
        if (m.kind == "agent") {
            Icon(Icons.Default.SmartToy, null, tint = hc.violet, modifier = Modifier.size((size * .58).dp))
        } else {
            Text(initials, fontSize = (size * .42).sp, fontWeight = FontWeight.SemiBold, color = hc.fg.copy(alpha = .8f))
        }
    }
}

@Composable
fun TaskRow(t: TaskItem, me: Member?, onToggle: () -> Unit, onOpen: () -> Unit) {
    val closed = t.status == "done" || t.status == "cancelled"
    val waiting = !closed && !t.ready && t.blocked_by.isNotEmpty() && t.claimed_by == null
    val showAssignee = t.assignee != null && t.assignee.id != me?.id && t.assignee.id != t.claimed_by?.id
    Surface(
        shape = RoundedCornerShape(16.dp),
        color = hc.card,
        border = androidx.compose.foundation.BorderStroke(1.dp, hc.border),
        modifier = Modifier.fillMaxWidth().clickable(onClick = onOpen),
    ) {
        Row(
            Modifier.padding(horizontal = 12.dp, vertical = 8.dp).height(40.dp).fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            StatusCircle(t, onToggle)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.Center) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    if (t.unseen && !closed) {
                        Box(Modifier.size(8.dp).clip(CircleShape).background(hc.unseen))
                    }
                    Text(
                        t.title,
                        fontSize = 15.sp,
                        fontWeight = FontWeight.Medium,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                        color = if (closed) hc.mutedFg.copy(alpha = .7f) else hc.fg,
                        textDecoration = if (closed) TextDecoration.LineThrough else null,
                    )
                    if (t.priority < 2 && !closed) {
                        Icon(
                            Icons.Default.Flag, null,
                            tint = if (t.priority == 0) Color(0xFFEF4444) else Color(0xFFF97316),
                            modifier = Modifier.size(14.dp),
                        )
                    }
                }
                val note = t.agent_note?.lineSequence()?.firstOrNull()
                if (note != null && !closed && t.triage != "pending") {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                        Icon(Icons.Default.SmartToy, null, tint = hc.violetFg, modifier = Modifier.size(12.dp))
                        Text(note, fontSize = 13.sp, color = hc.mutedFg, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                t.claimed_by?.takeIf { !closed }?.let { c ->
                    Row(
                        Modifier.clip(RoundedCornerShape(50)).background(hc.accent).padding(start = 8.dp, end = 2.dp, top = 2.dp, bottom = 2.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Box(Modifier.size(8.dp).clip(CircleShape)
                            .background(if (c.kind == "agent") Color(0xFF8B5CF6) else Color(0xFFF59E0B)))
                        Avatar(c, 20)
                    }
                }
                t.waiting_on?.takeIf { !closed }?.let { w ->
                    Pill(
                        if (w.id == me?.id) "Needs you" else "Needs ${w.name.split(" ").first()}",
                        hc.amberSoft, if (hc.bg == Color(0xFF0A0A0A)) Color(0xFFFBBF24) else Color(0xFFB45309),
                        icon = Icons.Outlined.ChatBubbleOutline,
                    )
                }
                if (waiting) {
                    Pill("Waiting", hc.amberSoft, Color(0xFFB45309), icon = Icons.Default.Lock)
                }
                if (showAssignee) Avatar(t.assignee!!, 24)
                if (t.repeat != null && !closed) {
                    Icon(Icons.Default.Repeat, null, tint = hc.mutedFg, modifier = Modifier.size(14.dp))
                }
                t.todo_progress?.let { p ->
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                        Icon(
                            Icons.Default.PlaylistAddCheck, null,
                            tint = if (p.done == p.total) Color(0xFF15803D) else hc.mutedFg,
                            modifier = Modifier.size(14.dp),
                        )
                        Text(
                            "${p.done}/${p.total}",
                            fontSize = 12.sp, color = if (p.done == p.total) Color(0xFF15803D) else hc.mutedFg,
                            style = TextStyle(fontFeatureSettings = "tnum"),
                        )
                    }
                }
                t.due?.let { DuePill(it, closed) }
            }
        }
    }
}

// ── Sectioned list (Overdue / Today / Upcoming / No date) ────────────────────

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TaskSections(
    api: Api,
    projectId: String? = null,
    refreshTick: Int,
    me: Member?,
    onOpen: (String) -> Unit,
    onSignedOut: () -> Unit,
    emptyText: String,
) {
    var error by remember { mutableStateOf<String?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    var loadTick by remember { mutableIntStateOf(0) }
    var tasks by remember { mutableStateOf<List<TaskItem>?>(null) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(refreshTick, loadTick) {
        refreshing = true
        callApi(onSignedOut, { error = it }) {
            if (projectId == null) api.myTasks() else api.searchTasks(projectId = projectId)
        }?.let { tasks = it }
        refreshing = false
    }

    fun toggle(t: TaskItem) {
        val done = t.status == "done" || t.status == "cancelled"
        tasks = tasks?.map { if (it.id == t.id) it.copy(status = if (done) "open" else "done") else it }
        scope.launch {
            callApi(onSignedOut, { error = it; loadTick++ }) {
                if (done) api.reopenTask(t.id) else api.close(t.id)
            }
        }
    }

    PullToRefreshBox(
        isRefreshing = refreshing,
        onRefresh = { loadTick++ },
        modifier = Modifier.fillMaxSize(),
    ) {
        LazyColumn(
            Modifier.fillMaxSize().padding(horizontal = 12.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item { Spacer(Modifier.height(4.dp)) }
            when {
                tasks == null -> item {
                    Box(Modifier.fillParentMaxHeight(.6f).fillMaxWidth()) {
                        CircularProgressIndicator(Modifier.align(Alignment.Center))
                    }
                }
                tasks!!.isEmpty() -> item {
                    Surface(
                        shape = RoundedCornerShape(16.dp), color = hc.card,
                        border = androidx.compose.foundation.BorderStroke(1.dp, hc.border),
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text(
                            emptyText, Modifier.padding(24.dp), fontSize = 14.sp,
                            color = hc.mutedFg, textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                        )
                    }
                }
                else -> groupByDue(tasks!!).forEach { s ->
                    item {
                        Row(
                            Modifier.padding(start = 4.dp, top = 8.dp),
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                            verticalAlignment = Alignment.Bottom,
                        ) {
                            Text(
                                s.title, fontSize = 13.sp, fontWeight = FontWeight.Medium,
                                color = if (s.key == "overdue") Color(0xFFDC2626) else hc.mutedFg,
                            )
                            Text(
                                "${s.tasks.size}", fontSize = 13.sp, color = hc.fg.copy(alpha = .35f),
                                style = TextStyle(fontFeatureSettings = "tnum"),
                            )
                        }
                    }
                    items(s.tasks, key = { it.id }) { t -> TaskRow(t, me, onToggle = { toggle(t) }, onOpen = { onOpen(t.id) }) }
                }
            }
            item { Spacer(Modifier.height(96.dp)) }
            error?.let { item { Text(it, color = hc.destructive, modifier = Modifier.padding(16.dp)) } }
        }
    }
}

// ── Chrome: top bar (org pill, bell, +) and the floating capsule nav ─────────

@Composable
fun TopBar(orgName: String, unread: Int, onBell: () -> Unit, onPlus: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Surface(
            shape = RoundedCornerShape(50), color = hc.card,
            border = androidx.compose.foundation.BorderStroke(1.dp, hc.border),
        ) {
            Text(
                orgName.ifBlank { "Handoff" },
                Modifier.padding(horizontal = 14.dp, vertical = 8.dp),
                fontSize = 15.sp, fontWeight = FontWeight.Medium,
            )
        }
        Spacer(Modifier.weight(1f))
        RoundIconButton(Icons.Default.Notifications, "Notifications", onBell, badge = unread)
        RoundIconButton(Icons.Default.Add, "New task", onPlus)
    }
}

@Composable
private fun RoundIconButton(icon: ImageVector, desc: String, onClick: () -> Unit, badge: Int = 0) {
    Surface(
        shape = CircleShape, color = hc.card,
        border = androidx.compose.foundation.BorderStroke(1.dp, hc.border),
        modifier = Modifier.size(40.dp).clip(CircleShape).clickable(onClick = onClick),
    ) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            Icon(icon, desc, tint = hc.fg, modifier = Modifier.size(20.dp))
            if (badge > 0) {
                Box(
                    Modifier.align(Alignment.TopEnd).padding(top = 4.dp, end = 4.dp)
                        .size(8.dp).clip(CircleShape).background(hc.unseen),
                )
            }
        }
    }
}

enum class Tab(val label: String, val icon: ImageVector) {
    Tasks("Tasks", Icons.Default.CheckBox),
    Inbox("Inbox", Icons.Default.Notifications),
    Projects("Projects", Icons.Default.Folder),
    More("More", Icons.Default.MoreHoriz),
}

@Composable
fun BottomNav(tab: Tab, unread: Int, onTab: (Tab) -> Unit) {
    Surface(
        shape = RoundedCornerShape(28.dp),
        color = hc.card.copy(alpha = .92f),
        border = androidx.compose.foundation.BorderStroke(1.dp, hc.border),
        shadowElevation = 8.dp,
        modifier = Modifier.padding(horizontal = 12.dp).fillMaxWidth().height(60.dp),
    ) {
        Row(Modifier.padding(4.dp), verticalAlignment = Alignment.CenterVertically) {
            Tab.entries.forEach { t ->
                val active = t == tab
                Column(
                    Modifier.weight(1f).clip(RoundedCornerShape(24.dp))
                        .background(if (active) hc.accent else Color.Transparent)
                        .clickable { onTab(t) }
                        .padding(vertical = 5.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Box {
                        Icon(t.icon, t.label, tint = if (active) hc.fg else hc.mutedFg, modifier = Modifier.size(20.dp))
                        if (t == Tab.Inbox && unread > 0) {
                            Badge(
                                Modifier.align(Alignment.TopEnd).padding(start = 14.dp, bottom = 10.dp),
                                containerColor = hc.unseen,
                            ) { Text("$unread", fontSize = 9.sp, color = Color(0xFF1A1A1A)) }
                        }
                    }
                    Text(t.label, fontSize = 11.sp, fontWeight = FontWeight.Medium,
                        color = if (active) hc.fg else hc.mutedFg)
                }
            }
        }
    }
}

@Composable
fun PageTitle(title: String) {
    Text(
        title, fontSize = 28.sp, fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 14.dp),
        color = hc.fg,
    )
}

// ── Inbox (notifications) ────────────────────────────────────────────────────

private val kindVerb = mapOf(
    "mention" to "mentioned you in",
    "assigned" to "assigned you",
    "approval" to "needs your approval on",
    "agent_done" to "finished",
)
private val kindIcon = mapOf(
    "mention" to Icons.Default.AlternateEmail,
    "assigned" to Icons.Default.PersonAdd,
    "approval" to Icons.Default.Shield,
    "agent_done" to Icons.Default.AutoAwesome,
)

private fun ago(at: String): String {
    val t = try {
        java.time.Instant.parse(at)
    } catch (_: Exception) {
        try { java.time.LocalDateTime.parse(at.removeSuffix("Z")).toInstant(java.time.ZoneOffset.UTC) } catch (_: Exception) { return "" }
    }
    val s = java.time.Duration.between(t, java.time.Instant.now()).seconds
    return when {
        s < 60 -> "just now"
        s < 3600 -> "${s / 60}m ago"
        s < 86400 -> "${s / 3600}h ago"
        s < 86400 * 7 -> "${s / 86400}d ago"
        else -> "${s / 604800}w ago"
    }
}

@Composable
fun InboxScreen(api: Api, refreshTick: Int, onOpen: (String) -> Unit, onSignedOut: () -> Unit, onRead: () -> Unit) {
    var error by remember { mutableStateOf<String?>(null) }
    var items by remember { mutableStateOf<List<Notification>?>(null) }
    var unread by remember { mutableIntStateOf(0) }
    var loadTick by remember { mutableIntStateOf(0) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(refreshTick, loadTick) {
        callApi(onSignedOut, { error = it }) { api.inbox() }?.let { (list, n) ->
            items = list
            unread = n
            // Opening the inbox reads it, like the web's Notifications page.
            if (list.any { !it.read }) {
                callApi(onSignedOut, {}) { api.markRead(all = true) }
                onRead()
            }
        }
    }

    Column(Modifier.fillMaxSize()) {
        PageTitle("Notifications")
        error?.let { Text(it, color = hc.destructive, modifier = Modifier.padding(horizontal = 16.dp)) }
        Surface(
            shape = RoundedCornerShape(16.dp), color = hc.card,
            border = androidx.compose.foundation.BorderStroke(1.dp, hc.border),
            modifier = Modifier.padding(horizontal = 12.dp).fillMaxWidth(),
        ) {
            when {
                items == null -> Box(Modifier.fillMaxWidth().padding(40.dp)) { CircularProgressIndicator(Modifier.align(Alignment.Center)) }
                items!!.isEmpty() -> Text(
                    "Nothing yet. When someone @mentions you, gives you a task, one of your agents needs approval, or the agent finishes something you asked for, it shows up here.",
                    Modifier.padding(horizontal = 16.dp, vertical = 32.dp),
                    fontSize = 14.sp, color = hc.mutedFg,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
                else -> Column {
                    items!!.forEachIndexed { i, n ->
                        Row(
                            Modifier.fillMaxWidth()
                                .background(if (n.read) Color.Transparent else hc.accent.copy(alpha = .5f))
                                .clickable(enabled = n.task_id.isNotEmpty()) {
                                    scope.launch {
                                        withContext(Dispatchers.IO) { runCatching { api.markRead(ids = listOf(n.id)) } }
                                    }
                                    if (n.task_id.isNotEmpty()) onOpen(n.task_id)
                                }
                                .padding(horizontal = 16.dp, vertical = 12.dp),
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            Box {
                                n.actor?.let { Avatar(it, 32) }
                                kindIcon[n.kind]?.let {
                                    Icon(it, null, tint = hc.mutedFg,
                                        modifier = Modifier.size(14.dp).align(Alignment.BottomEnd)
                                            .background(hc.card, CircleShape).padding(1.dp))
                                }
                            }
                            Column(Modifier.weight(1f)) {
                                Text(
                                    "${n.actor?.name ?: "Someone"} ${kindVerb[n.kind] ?: n.kind} ${n.task_title}",
                                    fontSize = 14.sp, color = hc.fg, maxLines = 2, overflow = TextOverflow.Ellipsis,
                                )
                                if (n.excerpt != null && n.kind != "assigned") {
                                    Text(n.excerpt, fontSize = 13.sp, color = hc.mutedFg, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                }
                                Text(ago(n.at), fontSize = 12.sp, color = hc.mutedFg.copy(alpha = .7f))
                            }
                            if (!n.read) {
                                Box(Modifier.padding(top = 8.dp).size(8.dp).clip(CircleShape).background(hc.primary))
                            }
                        }
                        if (i < items!!.lastIndex) {
                            Box(Modifier.fillMaxWidth().height(1.dp).background(hc.border))
                        }
                    }
                }
            }
        }
    }
}

// ── Projects ─────────────────────────────────────────────────────────────────

@Composable
fun ProjectsScreen(api: Api, refreshTick: Int, onOpen: (Project) -> Unit, onSignedOut: () -> Unit) {
    var error by remember { mutableStateOf<String?>(null) }
    var projects by remember { mutableStateOf<List<Project>?>(null) }
    var newName by remember { mutableStateOf("") }
    var loadTick by remember { mutableIntStateOf(0) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(refreshTick, loadTick) {
        callApi(onSignedOut, { error = it }) { api.projects() }?.let { projects = it }
    }

    Column(Modifier.fillMaxSize()) {
        PageTitle("Projects")
        error?.let { Text(it, color = hc.destructive, modifier = Modifier.padding(horizontal = 16.dp)) }
        Surface(
            shape = RoundedCornerShape(16.dp), color = hc.card,
            border = androidx.compose.foundation.BorderStroke(1.dp, hc.border),
            modifier = Modifier.padding(horizontal = 12.dp).fillMaxWidth(),
        ) {
            Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                androidx.compose.material3.OutlinedTextField(
                    value = newName, onValueChange = { newName = it },
                    placeholder = { Text("New project name") }, singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                    shape = RoundedCornerShape(12.dp),
                )
                androidx.compose.material3.Button(
                    onClick = {
                        scope.launch {
                            callApi(onSignedOut, { error = it }) {
                                api.createProject(newName.trim())
                            }?.let { newName = ""; loadTick++ }
                        }
                    },
                    enabled = newName.isNotBlank(),
                    modifier = Modifier.fillMaxWidth(),
                    colors = androidx.compose.material3.ButtonDefaults.buttonColors(
                        containerColor = hc.primary, contentColor = hc.primaryFg),
                ) { Text("Create") }
            }
        }
        Spacer(Modifier.height(12.dp))
        Surface(
            shape = RoundedCornerShape(16.dp), color = hc.card,
            border = androidx.compose.foundation.BorderStroke(1.dp, hc.border),
            modifier = Modifier.padding(horizontal = 12.dp).fillMaxWidth(),
        ) {
            Column {
                projects?.forEachIndexed { i, p ->
                    Row(
                        Modifier.fillMaxWidth().clickable { onOpen(p) }.padding(horizontal = 16.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(p.name, fontSize = 14.sp, fontWeight = FontWeight.Medium, color = hc.fg, modifier = Modifier.weight(1f))
                        Text("${p.open_tasks} open", fontSize = 14.sp, color = hc.mutedFg)
                    }
                    if (projects != null && i < projects!!.lastIndex) {
                        Box(Modifier.fillMaxWidth().height(1.dp).background(hc.border))
                    }
                }
            }
        }
    }
}

// ── More ─────────────────────────────────────────────────────────────────────

@Composable
fun MoreScreen(member: Member?, orgName: String, onSignOut: () -> Unit) {
    Column(Modifier.fillMaxSize()) {
        PageTitle("More")
        Surface(
            shape = RoundedCornerShape(16.dp), color = hc.card,
            border = androidx.compose.foundation.BorderStroke(1.dp, hc.border),
            modifier = Modifier.padding(horizontal = 12.dp).fillMaxWidth(),
        ) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                member?.let {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        Avatar(it, 40)
                        Column {
                            Text(it.name, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, color = hc.fg)
                            Text(it.email, fontSize = 13.sp, color = hc.mutedFg)
                        }
                    }
                    Spacer(Modifier.height(8.dp))
                }
                Text(orgName, fontSize = 13.sp, color = hc.mutedFg)
                Spacer(Modifier.height(12.dp))
                Text(
                    "Sign out",
                    fontSize = 15.sp, fontWeight = FontWeight.Medium, color = hc.destructive,
                    modifier = Modifier.clip(RoundedCornerShape(8.dp)).clickable(onClick = onSignOut).padding(vertical = 8.dp),
                )
            }
        }
    }
}
