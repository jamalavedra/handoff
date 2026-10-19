package app.handoff

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
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
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import java.time.OffsetDateTime
import java.time.format.DateTimeFormatter
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TaskDetailScreen(
    api: Api,
    taskId: String,
    me: Member?,
    onBack: () -> Unit,
    onSignedOut: () -> Unit,
) {
    BackHandler(onBack = onBack)
    var task by remember { mutableStateOf<TaskItem?>(null) }
    var comments by remember { mutableStateOf<List<Comment>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var loadTick by remember { mutableIntStateOf(0) }
    var draft by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    fun reload() = loadTick++

    LaunchedEffect(taskId, loadTick) {
        callApi(onSignedOut, { error = it }) { api.task(taskId) }?.let { (t, c) ->
            task = t
            comments = c
        }
    }

    fun act(block: Api.() -> Unit) {
        scope.launch {
            busy = true
            callApi(onSignedOut, { error = it }) { api.block() }
            busy = false
            reload()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(task?.id ?: "") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
            )
        },
    ) { pad ->
        val t = task
        if (t == null) {
            Box(Modifier.fillMaxSize().padding(pad)) {
                if (error != null) {
                    Column(Modifier.align(Alignment.Center)) {
                        Text(error!!, color = MaterialTheme.colorScheme.error)
                        Spacer(Modifier.height(12.dp))
                        Button(onClick = { error = null; reload() }) { Text("Retry") }
                    }
                } else {
                    CircularProgressIndicator(Modifier.align(Alignment.Center))
                }
            }
            return@Scaffold
        }
        Column(Modifier.fillMaxSize().padding(pad)) {
            LazyColumn(Modifier.weight(1f).padding(horizontal = 16.dp)) {
                item {
                    Text(t.title, style = MaterialTheme.typography.titleLarge)
                    Spacer(Modifier.height(4.dp))
                    Text(
                        listOfNotNull(
                            t.status.replace('_', ' '),
                            t.project.ifBlank { null },
                            t.due,
                            t.assignee?.let { "→ ${it.name}" },
                            t.claimed_by?.let { "claimed by ${it.name}" },
                        ).joinToString("  ·  "),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    if (t.body.isNotBlank()) {
                        Spacer(Modifier.height(8.dp))
                        Text(t.body, style = MaterialTheme.typography.bodyMedium)
                    }
                    Spacer(Modifier.height(12.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        val mine = t.claimed_by?.id == me?.id
                        if (t.status != "done" && t.status != "cancelled") {
                            if (t.claimed_by == null) {
                                OutlinedButton(enabled = !busy, onClick = { act { claim(t.id) } }) {
                                    Text("Claim")
                                }
                            } else if (mine) {
                                OutlinedButton(enabled = !busy, onClick = { act { release(t.id) } }) {
                                    Text("Release")
                                }
                            }
                            Button(enabled = !busy, onClick = {
                                scope.launch {
                                    busy = true
                                    val done = callApi(onSignedOut, { error = it }) { api.close(t.id) }
                                    busy = false
                                    if (done != null) onBack()
                                }
                            }) {
                                Text("Mark done")
                            }
                        }
                    }
                    if (t.todos.isNotEmpty()) {
                        Spacer(Modifier.height(16.dp))
                        Text("Steps", style = MaterialTheme.typography.titleSmall)
                    }
                }
                items(t.todos, key = { it.id }) { todo ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(
                            checked = todo.done,
                            enabled = !busy,
                            onCheckedChange = { done -> act { setTodoDone(todo.id, done) } },
                        )
                        Column {
                            Text(
                                todo.text,
                                textDecoration = if (todo.done) TextDecoration.LineThrough else null,
                            )
                            todo.result?.let {
                                Text(it, style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                    }
                }
                item {
                    Spacer(Modifier.height(16.dp))
                    HorizontalDivider()
                    Spacer(Modifier.height(8.dp))
                    Text("Comments", style = MaterialTheme.typography.titleSmall)
                    Spacer(Modifier.height(8.dp))
                }
                items(comments, key = { it.id }) { c ->
                    Column(Modifier.padding(vertical = 6.dp)) {
                        Text(
                            "${c.author?.name ?: "?"}  ·  ${fmtTime(c.created_at)}",
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Text(c.body, style = MaterialTheme.typography.bodyMedium)
                    }
                }
            }
            error?.let {
                Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(horizontal = 16.dp))
            }
            Row(Modifier.fillMaxWidth().padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = draft,
                    onValueChange = { draft = it },
                    placeholder = { Text("Comment…") },
                    modifier = Modifier.weight(1f),
                )
                Spacer(Modifier.width(8.dp))
                Button(
                    enabled = !busy && draft.isNotBlank(),
                    onClick = {
                        val body = draft
                        scope.launch {
                            busy = true
                            val sent = callApi(onSignedOut, { error = it }) { api.comment(t.id, body) }
                            busy = false
                            if (sent != null) {
                                draft = ""
                                reload()
                            }
                        }
                    },
                ) { Text("Send") }
            }
        }
    }
}

private fun fmtTime(iso: String): String = try {
    OffsetDateTime.parse(iso).format(DateTimeFormatter.ofPattern("MMM d, HH:mm"))
} catch (_: Exception) {
    iso.take(10)
}
