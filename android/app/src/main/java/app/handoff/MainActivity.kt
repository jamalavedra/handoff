package app.handoff

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent { HandoffTheme { Surface(Modifier.fillMaxSize(), color = hc.bg) { Root() } } }
    }
}

private sealed interface Screen {
    data object List : Screen
    data class Detail(val id: String) : Screen
    data class ProjectTasks(val project: Project) : Screen
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Root() {
    val ctx = LocalContext.current
    val session = remember { Session(ctx) }

    var signedIn by remember { mutableStateOf<Boolean?>(null) }
    var member by remember { mutableStateOf<Member?>(null) }
    var orgName by remember { mutableStateOf("") }
    var screen by remember { mutableStateOf<Screen>(Screen.List) }
    var tab by remember { mutableStateOf(Tab.Tasks) }
    var refreshTick by remember { mutableIntStateOf(0) }
    var unread by remember { mutableIntStateOf(0) }
    var quickAdd by remember { mutableStateOf(false) }
    var bootError by remember { mutableStateOf(false) }
    var bootTick by remember { mutableIntStateOf(0) }
    val scope = rememberCoroutineScope()

    fun signOut() {
        scope.launch {
            val api = session.api
            runCatching { withContext(Dispatchers.IO) { api?.logout() } }
            session.clear()
            member = null
            signedIn = false
            screen = Screen.List
            tab = Tab.Tasks
        }
    }

    fun backToList() {
        screen = Screen.List
        refreshTick++
    }

    LaunchedEffect(bootTick) {
        val api = session.api
        if (api == null || session.cookie == null) {
            signedIn = false
            return@LaunchedEffect
        }
        val me = try {
            withContext(Dispatchers.IO) { api.me() }
        } catch (e: AuthException) {
            null
        } catch (e: Exception) {
            signedIn = false
            bootError = true // unreachable host; cookie kept so retry can reuse it
            return@LaunchedEffect
        }
        if (me?.member != null) {
            member = me.member
            orgName = me.orgName
            signedIn = true
        } else {
            session.clear()
            signedIn = false
        }
    }

    // Unread count for the Inbox badge; refreshed whenever the list reloads.
    LaunchedEffect(signedIn, refreshTick, tab) {
        if (signedIn == true) {
            callApi(::signOut, {}) { session.api!!.inbox() }?.let { unread = it.second }
        }
    }

    when (signedIn) {
        null -> Box(Modifier.fillMaxSize()) { CircularProgressIndicator(Modifier.align(Alignment.Center)) }
        false -> Column(Modifier.fillMaxSize().statusBarsPadding()) {
            if (bootError) {
                Text("Couldn't reach your workspace",
                    color = hc.destructive,
                    modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp))
                TextButton(onClick = {
                    bootError = false
                    signedIn = null
                    bootTick++ // re-run the boot probe against the saved cookie
                }) { Text("Retry with saved sign-in") }
            }
            SignInScreen(session) {
                member = it.member
                orgName = it.orgName
                signedIn = true
            }
        }
        true -> Box(Modifier.fillMaxSize()) {
            Column(Modifier.fillMaxSize().statusBarsPadding()) {
                when (val s = screen) {
                    is Screen.Detail -> TaskDetailScreen(
                        api = session.api!!,
                        taskId = s.id,
                        me = member,
                        onBack = ::backToList,
                        onSignedOut = ::signOut,
                    )
                    is Screen.ProjectTasks -> {
                        BackHandler(onBack = ::backToList)
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            IconButton(onClick = ::backToList) {
                                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                            }
                            PageTitle(s.project.name)
                        }
                        Box(Modifier.weight(1f)) {
                            TaskSections(
                                api = session.api!!,
                                projectId = s.project.id,
                                refreshTick = refreshTick,
                                me = member,
                                onOpen = { screen = Screen.Detail(it) },
                                onSignedOut = ::signOut,
                                emptyText = "No tasks here yet.",
                            )
                        }
                    }
                    Screen.List -> {
                        TopBar(orgName, unread,
                            onBell = { tab = Tab.Inbox },
                            onPlus = { quickAdd = true })
                        Column(Modifier.weight(1f)) {
                            when (tab) {
                                Tab.Tasks -> {
                                    PageTitle("My tasks")
                                    TaskSections(
                                        api = session.api!!,
                                        refreshTick = refreshTick,
                                        me = member,
                                        onOpen = { screen = Screen.Detail(it) },
                                        onSignedOut = ::signOut,
                                        emptyText = "Nothing assigned to you. Agents can assign tasks to you too.",
                                    )
                                }
                                Tab.Inbox -> InboxScreen(session.api!!, refreshTick,
                                    onOpen = { screen = Screen.Detail(it) },
                                    onSignedOut = ::signOut,
                                    onRead = { unread = 0 })
                                Tab.Projects -> ProjectsScreen(session.api!!, refreshTick,
                                    onOpen = { screen = Screen.ProjectTasks(it) },
                                    onSignedOut = ::signOut)
                                Tab.More -> MoreScreen(member, orgName, ::signOut)
                            }
                        }
                    }
                }
            }
            if (screen == Screen.List) {
                Box(Modifier.align(Alignment.BottomCenter).navigationBarsPadding()) {
                    BottomNav(tab, unread) { tab = it }
                }
            }
            if (quickAdd) {
                ModalBottomSheet(onDismissRequest = { quickAdd = false }) {
                    QuickAddSheet(api = session.api!!, onSignedOut = ::signOut) {
                        quickAdd = false
                        refreshTick++
                    }
                }
            }
        }
    }
}
