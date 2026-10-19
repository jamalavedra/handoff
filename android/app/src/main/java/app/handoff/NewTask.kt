package app.handoff

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

private val dueRe = Regex("\\d{4}-\\d{2}-\\d{2}")

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NewTaskScreen(api: Api, onDone: () -> Unit, onSignedOut: () -> Unit) {
    BackHandler(onBack = onDone)
    var title by remember { mutableStateOf("") }
    var project by remember { mutableStateOf<String?>(null) }
    var projects by remember { mutableStateOf<List<Project>>(emptyList()) }
    var due by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var menuOpen by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) {
        callApi(onSignedOut, { error = it }) { api.projects() }?.let { projects = it }
    }

    val dueBad = due.isNotBlank() && !due.matches(dueRe)

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("New task") },
                navigationIcon = {
                    IconButton(onClick = onDone) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
            )
        },
    ) { pad ->
        Column(Modifier.fillMaxSize().padding(pad).padding(24.dp)) {
            OutlinedTextField(
                value = title,
                onValueChange = { title = it },
                label = { Text("Title") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(16.dp))
            Box {
                OutlinedButton(onClick = { menuOpen = true }, modifier = Modifier.fillMaxWidth()) {
                    Text(project ?: "Project (optional)")
                }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    DropdownMenuItem(text = { Text("Inbox") }, onClick = {
                        project = null
                        menuOpen = false
                    })
                    projects.forEach { p ->
                        DropdownMenuItem(text = { Text(p.name) }, onClick = {
                            project = p.name
                            menuOpen = false
                        })
                    }
                }
            }
            Spacer(Modifier.height(16.dp))
            OutlinedTextField(
                value = due,
                onValueChange = { due = it },
                label = { Text("Due (YYYY-MM-DD, optional)") },
                singleLine = true,
                isError = dueBad,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(24.dp))
            Button(
                enabled = !busy && title.isNotBlank() && !dueBad,
                modifier = Modifier.fillMaxWidth(),
                onClick = {
                    scope.launch {
                        busy = true
                        callApi(onSignedOut, { error = it }) {
                            api.createTask(title.trim(), project.orEmpty(), due)
                        }?.let { onDone() }
                        busy = false
                    }
                },
            ) { Text("Create task") }
            error?.let {
                Spacer(Modifier.height(12.dp))
                Text(it, color = MaterialTheme.colorScheme.error)
            }
        }
    }
}
