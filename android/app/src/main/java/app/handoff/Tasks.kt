package app.handoff

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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
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
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import java.time.LocalDate

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TaskListScreen(
    api: Api,
    readyOnly: Boolean,
    refreshTick: Int,
    onOpen: (String) -> Unit,
    onSignedOut: () -> Unit,
) {
    var error by remember { mutableStateOf<String?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    var loadTick by remember { mutableIntStateOf(0) }
    var tasks by remember { mutableStateOf<List<TaskItem>?>(null) }

    LaunchedEffect(refreshTick, loadTick, readyOnly) {
        refreshing = true
        callApi(onSignedOut, { error = it }) {
            if (readyOnly) api.readyTasks() else api.searchTasks()
        }?.let { tasks = it }
        refreshing = false
    }

    PullToRefreshBox(
        isRefreshing = refreshing,
        onRefresh = { loadTick++ },
        modifier = Modifier.fillMaxSize(),
    ) {
        when {
            tasks == null -> Box(Modifier.fillMaxSize()) {
                CircularProgressIndicator(Modifier.align(Alignment.Center))
            }
            tasks!!.isEmpty() -> Box(Modifier.fillMaxSize()) {
                Text(
                    if (readyOnly) "Nothing ready to start" else "No open tasks",
                    Modifier.align(Alignment.Center),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            else -> LazyColumn(Modifier.fillMaxSize()) {
                items(tasks!!, key = { it.id }) { t -> TaskRow(t) { onOpen(t.id) } }
            }
        }
        error?.let {
            Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(16.dp))
        }
    }
}

@Composable
private fun TaskRow(t: TaskItem, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    t.title,
                    style = MaterialTheme.typography.bodyLarge,
                    fontWeight = if (t.unseen) FontWeight.Bold else FontWeight.Normal,
                    textDecoration = if (t.status == "done") TextDecoration.LineThrough else null,
                    modifier = Modifier.weight(1f, fill = false),
                )
                if (t.claimed_by?.kind == "agent") {
                    Spacer(Modifier.size(6.dp))
                    Surface(
                        color = MaterialTheme.colorScheme.tertiary,
                        shape = CircleShape,
                        modifier = Modifier.size(8.dp),
                    ) {}
                }
            }
            val sub = listOfNotNull(
                t.project.ifBlank { null },
                t.due?.let { dueText(it) },
                t.assignee?.name,
                t.todo_progress?.let { "${it.done}/${it.total} done" },
            ).joinToString("  ·  ")
            if (sub.isNotEmpty()) {
                Text(
                    sub,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

private fun dueText(due: String): String {
    val d = try {
        LocalDate.parse(due.take(10))
    } catch (_: Exception) {
        return due
    }
    val today = LocalDate.now()
    return when (d) {
        today -> "today"
        today.plusDays(1) -> "tomorrow"
        else -> if (d.isBefore(today)) "overdue ${d}" else d.toString()
    }
}

@Composable
fun AccountScreen(member: Member?, orgName: String, onSignOut: () -> Unit) {
    Column(Modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.Center) {
        Text(member?.name.orEmpty(), style = MaterialTheme.typography.headlineSmall)
        Text(member?.email.orEmpty(), style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(orgName, style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.height(24.dp))
        Button(onClick = onSignOut) { Text("Sign out") }
    }
}
