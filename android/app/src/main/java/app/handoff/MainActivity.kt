package app.handoff

import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Person
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
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

enum class Tab { Tasks, Ready, Account }

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent { HandoffApp() }
    }
}

@Composable
fun HandoffApp() {
    val dark = isSystemInDarkTheme()
    val ctx = LocalContext.current
    val scheme = when {
        Build.VERSION.SDK_INT >= 31 -> if (dark) dynamicDarkColorScheme(ctx) else dynamicLightColorScheme(ctx)
        dark -> darkColorScheme()
        else -> lightColorScheme()
    }
    MaterialTheme(colorScheme = scheme) {
        Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
            Root()
        }
    }
}

private sealed interface Screen {
    data object List : Screen
    data class Detail(val id: String) : Screen
    data object NewTask : Screen
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

    var bootError by remember { mutableStateOf(false) }
    var bootTick by remember { mutableIntStateOf(0) }

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

    when (signedIn) {
        null -> Box(Modifier.fillMaxSize()) { CircularProgressIndicator(Modifier.align(Alignment.Center)) }
        false -> Column(Modifier.fillMaxSize()) {
            if (bootError) {
                Text("Couldn't reach your workspace",
                    color = MaterialTheme.colorScheme.error,
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
        true -> when (val s = screen) {
            is Screen.Detail -> TaskDetailScreen(
                api = session.api!!,
                taskId = s.id,
                me = member,
                onBack = { screen = Screen.List; refreshTick++ },
                onSignedOut = ::signOut,
            )
            Screen.NewTask -> NewTaskScreen(
                api = session.api!!,
                onDone = { screen = Screen.List; refreshTick++ },
                onSignedOut = ::signOut,
            )
            Screen.List -> Scaffold(
                topBar = { TopAppBar(title = { Text(orgName.ifBlank { "Handoff" }) }) },
                floatingActionButton = {
                    if (tab != Tab.Account) {
                        FloatingActionButton(onClick = { screen = Screen.NewTask }) {
                            Icon(Icons.Default.Add, contentDescription = "New task")
                        }
                    }
                },
                bottomBar = {
                    NavigationBar {
                        NavigationBarItem(tab == Tab.Tasks, { tab = Tab.Tasks },
                            icon = { Icon(Icons.AutoMirrored.Filled.List, null) }, label = { Text("Tasks") })
                        NavigationBarItem(tab == Tab.Ready, { tab = Tab.Ready },
                            icon = { Icon(Icons.Default.CheckCircle, null) }, label = { Text("Ready") })
                        NavigationBarItem(tab == Tab.Account, { tab = Tab.Account },
                            icon = { Icon(Icons.Default.Person, null) }, label = { Text("You") })
                    }
                },
            ) { pad ->
                Surface(Modifier.padding(pad)) {
                    when (tab) {
                        Tab.Tasks -> TaskListScreen(session.api!!, readyOnly = false, refreshTick,
                            onOpen = { screen = Screen.Detail(it) }, onSignedOut = ::signOut)
                        Tab.Ready -> TaskListScreen(session.api!!, readyOnly = true, refreshTick,
                            onOpen = { screen = Screen.Detail(it) }, onSignedOut = ::signOut)
                        Tab.Account -> AccountScreen(member, orgName, ::signOut)
                    }
                }
            }
        }
    }
}
