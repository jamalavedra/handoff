package app.handoff

import android.webkit.CookieManager
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Sign-in: enter the workspace host, then either let the server email a magic link
 * (paste it back here — the emailed link itself opens the system browser), or use
 * the full web flow in an embedded WebView whose CookieManager we watch.
 */
@Composable
fun SignInScreen(session: Session, onSignedIn: (Api.Me) -> Unit) {
    var host by remember { mutableStateOf(session.host ?: "") }
    var hostOk by remember { mutableStateOf(session.host != null) }
    var orgName by remember { mutableStateOf("") }
    var email by remember { mutableStateOf("") }
    var link by remember { mutableStateOf("") }
    var code by remember { mutableStateOf("") }
    var codeMode by remember { mutableStateOf(false) }
    var sent by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var webOpen by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    fun fail(msg: String?) {
        error = msg
        busy = false
    }

    fun finish(api: Api) {
        scope.launch {
            busy = true
            val me = try {
                withContext(Dispatchers.IO) { api.me() }
            } catch (e: Exception) {
                return@launch fail("signed in, but couldn't load the workspace")
            }
            if (me.member == null) {
                fail("sign-in didn't take")
            } else {
                onSignedIn(me)
            }
        }
    }

    fun redeem(raw: String) {
        val token = linkToken(raw)
        if (token == null) {
            fail("that doesn't look like a sign-in link")
            return
        }
        scope.launch {
            busy = true
            error = null
            // A pasted link carries its own workspace host; redeem there so the
            // token never crosses workspaces (and a wrong typed host still works).
            val linkHost = Regex("https?://[^/\\s]+").find(raw)?.value?.let { normalizeHost(it) }
            val host = linkHost ?: session.host!!
            if (linkHost != null && linkHost != session.host) {
                session.host = linkHost
            }
            val api = Api(host, null)
            try {
                val cookie = withContext(Dispatchers.IO) { api.verifyToken(token) }
                api.cookie = cookie
                session.cookie = cookie
                finish(api)
            } catch (e: Exception) {
                fail(e.message)
            }
        }
    }

    Column(
        Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Text("Handoff", style = MaterialTheme.typography.headlineMedium)
        Text("The shared task list for people and agents", style = MaterialTheme.typography.bodyMedium)
        Spacer(Modifier.height(24.dp))

        if (!hostOk) {
            Text("Your workspace", style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(8.dp))
            OutlinedTextField(
                value = host,
                onValueChange = { host = it },
                placeholder = { Text("acme.handoff.app") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(16.dp))
            Button(
                enabled = !busy && host.isNotBlank(),
                modifier = Modifier.fillMaxWidth(),
                onClick = {
                    scope.launch {
                        busy = true
                        error = null
                        val h = normalizeHost(host)
                        try {
                            val me = withContext(Dispatchers.IO) { Api(h, null).me() }
                            if (me.apex) {
                                fail("that's the signup site — use a workspace host, e.g. <slug>.${h.substringAfter("://")}")
                            } else {
                                host = h
                                orgName = me.orgName
                                session.host = h
                                hostOk = true
                                busy = false
                            }
                        } catch (e: Exception) {
                            fail("can't reach $h")
                        }
                    }
                },
            ) { Text("Continue") }
        } else {
            Text("Sign in to ${orgName.ifBlank { session.host?.substringAfter("://").orEmpty() }}",
                style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(8.dp))
            OutlinedTextField(
                value = email,
                onValueChange = { email = it },
                placeholder = { Text("you@work.com") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email),
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(16.dp))
            Button(
                enabled = !busy && email.contains("@"),
                modifier = Modifier.fillMaxWidth(),
                onClick = {
                    scope.launch {
                        busy = true
                        error = null
                        val api = Api(session.host!!, null)
                        try {
                            val devLink = withContext(Dispatchers.IO) { api.login(email.trim()) }
                            if (devLink != null) {
                                redeem(devLink) // dev mode: the link comes straight back
                            } else {
                                sent = true
                                busy = false
                            }
                        } catch (e: Exception) {
                            fail(e.message)
                        }
                    }
                },
            ) { Text(if (sent) "Resend link" else "Email me a sign-in link") }

            if (sent) {
                Spacer(Modifier.height(16.dp))
                Text("Check your email. The link opens in your phone's browser, so paste it here instead:")
                Spacer(Modifier.height(8.dp))
            }
            OutlinedTextField(
                value = link,
                onValueChange = { link = it },
                placeholder = { Text("Paste your sign-in link") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            if (link.isNotBlank()) {
                Spacer(Modifier.height(8.dp))
                Button(enabled = !busy, modifier = Modifier.fillMaxWidth(), onClick = { redeem(link) }) {
                    Text("Sign in")
                }
            }
            if (codeMode) {
                Spacer(Modifier.height(16.dp))
                Text("On the web app: Settings → Pair a device gives you a code.")
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = code,
                    onValueChange = { code = it.uppercase().filter(Char::isLetterOrDigit).take(8) },
                    placeholder = { Text("XXXX-XXXX") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(8.dp))
                Button(
                    enabled = !busy && code.length == 8,
                    modifier = Modifier.fillMaxWidth(),
                    onClick = {
                        scope.launch {
                            busy = true
                            error = null
                            val api = Api(session.host!!, null)
                            try {
                                withContext(Dispatchers.IO) { api.redeemCode(code) }
                                session.cookie = api.cookie
                                finish(api)
                            } catch (e: ApiException) {
                                fail(if (e.code == 429) e.message else "invalid or expired code")
                            } catch (e: Exception) {
                                fail(e.message)
                            }
                        }
                    },
                ) { Text("Sign in with code") }
            }
            Spacer(Modifier.height(8.dp))
            TextButton(enabled = !busy, onClick = { codeMode = !codeMode }) {
                Text(if (codeMode) "Hide code sign-in" else "Have a code?")
            }
            TextButton(enabled = !busy, onClick = { webOpen = true }) {
                Text("Or sign in with the web flow")
            }
            TextButton(enabled = !busy, onClick = { hostOk = false; session.host = null }) {
                Text("Use a different workspace")
            }
        }

        error?.let {
            Spacer(Modifier.height(12.dp))
            Text(it, color = MaterialTheme.colorScheme.error)
        }
        if (busy) {
            Spacer(Modifier.height(12.dp))
            CircularProgressIndicator()
        }
    }

    if (webOpen) {
        WebSignIn(
            host = session.host!!,
            onClose = { webOpen = false },
            onCookie = { cookie ->
                webOpen = false
                session.cookie = cookie
                finish(Api(session.host!!, cookie))
            },
        )
    }
}

/** The hosted web sign-in (incl. Google OAuth). Captures handoff_session once it lands. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WebSignIn(host: String, onCookie: (String) -> Unit, onClose: () -> Unit) {
    Dialog(
        onDismissRequest = onClose,
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Surface(Modifier.fillMaxSize()) {
            Column {
                TopAppBar(
                    title = { Text("Sign in") },
                    navigationIcon = {
                        IconButton(onClick = onClose) { Icon(Icons.Default.Close, contentDescription = "Close") }
                    },
                )
                AndroidView(
                    modifier = Modifier.fillMaxSize(),
                    factory = { ctx ->
                        WebView(ctx).apply {
                            settings.javaScriptEnabled = true
                            settings.domStorageEnabled = true
                            val cm = CookieManager.getInstance()
                            cm.setAcceptCookie(true)
                            webViewClient = object : WebViewClient() {
                                override fun onPageFinished(view: WebView, url: String) {
                                    val raw = cm.getCookie("$host/") ?: return
                                    val cookie = raw.split(";").map { it.trim() }
                                        .firstOrNull { it.startsWith("handoff_session=") }
                                        ?.substringAfter("=")
                                        ?.takeIf { it.isNotEmpty() } ?: return
                                    onCookie(cookie)
                                }
                            }
                            loadUrl("$host/login")
                        }
                    },
                )
            }
        }
    }
}
