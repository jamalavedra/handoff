package app.handoff

import java.io.IOException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import okhttp3.FormBody
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

class AuthException : IOException("signed out")

class ApiException(val code: Int, message: String) : IOException(message)

/** Runs a blocking call on IO; 401 → onSignedOut, other failures → onError. */
suspend fun <T> callApi(onSignedOut: () -> Unit, onError: (String) -> Unit, block: () -> T): T? = try {
    withContext(Dispatchers.IO) { block() }
} catch (e: AuthException) {
    onSignedOut()
    null
} catch (e: Exception) {
    onError(e.message ?: "request failed")
    null
}

@Serializable
data class Member(
    val id: String,
    val name: String,
    val kind: String = "human",
    val email: String = "",
    val role: String = "",
)

@Serializable
data class Todo(
    val id: String,
    val text: String,
    val done: Boolean = false,
    val position: Int = 0,
    val result: String? = null,
)

@Serializable
data class TodoProgress(val done: Int, val total: Int)

@Serializable
data class TaskItem(
    val id: String,
    val title: String,
    val body: String = "",
    val status: String,
    val priority: Int = 2,
    val project: String = "",
    val project_id: String = "",
    val assignee: Member? = null,
    val due: String? = null,
    val labels: List<String> = emptyList(),
    val claimed_by: Member? = null,
    val created_by: Member? = null,
    val ready: Boolean = false,
    val unseen: Boolean = false,
    val todos: List<Todo> = emptyList(),
    val todo_progress: TodoProgress? = null,
    val agent_note: String? = null,
    val url: String = "",
)

@Serializable
data class Comment(
    val id: String,
    val author: Member? = null,
    val body: String,
    val created_at: String = "",
)

@Serializable
data class Project(val id: String, val name: String, val open_tasks: Int = 0)

/** Client for the same HTTP API the web SPA uses: session cookie + X-Handoff CSRF header. */
class Api(private val host: String, var cookie: String?) {
    private val client = OkHttpClient()
    private val json = Json { ignoreUnknownKeys = true }

    class Me(val member: Member?, val orgName: String, val orgSlug: String, val apex: Boolean)

    private fun parseError(body: String, fallback: String): String = try {
        json.parseToJsonElement(body).jsonObject["error"]?.let { it.toString().trim('"') } ?: fallback
    } catch (_: Exception) {
        fallback
    }

    private fun execute(req: Request): Pair<Int, String> {
        client.newCall(req).execute().use { resp ->
            val body = resp.body?.string().orEmpty()
            if (resp.code == 401) throw AuthException()
            if (!resp.isSuccessful) throw ApiException(resp.code, parseError(body, "HTTP ${resp.code}"))
            return resp.code to body
        }
    }

    private fun http(path: String, method: String = "GET", jsonBody: JsonObject? = null): String {
        val body = when {
            jsonBody != null -> jsonBody.toString().toRequestBody("application/json".toMediaType())
            method == "POST" -> "{}".toRequestBody("application/json".toMediaType())
            else -> null
        }
        val req = Request.Builder()
            .url("$host$path")
            .method(method, body)
            .header("X-Handoff", "1")
            .apply { cookie?.let { header("Cookie", "handoff_session=$it") } }
            .build()
        return execute(req).second
    }

    /** Reachable workspace? Returns the /api/me payload basics (member may be null pre-sign-in). */
    fun me(): Me {
        val o = json.parseToJsonElement(http("/api/me")).jsonObject
        val org = o["org"]?.jsonObject
        val member = o["member"]?.takeIf { it !is kotlinx.serialization.json.JsonNull }
            ?.let { json.decodeFromJsonElement(Member.serializer(), it) }
        return Me(
            member = member,
            orgName = org?.get("name")?.toString()?.trim('"').orEmpty(),
            orgSlug = org?.get("slug")?.toString()?.trim('"').orEmpty(),
            apex = o["apex"]?.toString() == "true",
        )
    }

    /** Emails the magic link; in dev mode returns the link itself. */
    fun login(email: String): String? {
        val out = http("/api/login", "POST", buildJsonObject { put("email", email); put("next", "/") })
        return json.parseToJsonElement(out).jsonObject["dev_link"]?.toString()?.trim('"')
    }

    /** Redeems a magic-link token for the session cookie. The cookie travels as Set-Cookie. */
    fun verifyToken(token: String): String {
        val req = Request.Builder()
            .url("$host/auth/verify")
            .post(FormBody.Builder().add("token", token).build())
            .build()
        client.newBuilder().followRedirects(false).build().newCall(req).execute().use { resp ->
            val c = resp.headers("Set-Cookie")
                .mapNotNull { Regex("handoff_session=([^;]*)").find(it)?.groupValues?.get(1) }
                .firstOrNull { it.isNotEmpty() }
            if (c != null) return c
            if (resp.code in 300..399) throw ApiException(resp.code, "link expired or already used")
            throw ApiException(resp.code, "sign-in failed (${resp.code})")
        }
    }

    private fun tool(name: String, args: JsonObject = JsonObject(emptyMap())): JsonObject =
        json.parseToJsonElement(http("/api/tools/$name", "POST", args)).jsonObject

    private fun tasks(key: String, out: JsonObject): List<TaskItem> =
        (out[key] as? kotlinx.serialization.json.JsonArray)
            ?.map { json.decodeFromJsonElement(TaskItem.serializer(), it) } ?: emptyList()

    fun searchTasks(query: String = ""): List<TaskItem> =
        tasks("tasks", tool("search_tasks", buildJsonObject { put("query", query); put("limit", 500) }))

    fun readyTasks(): List<TaskItem> = tasks("tasks", tool("list_ready", buildJsonObject { put("limit", 100) }))

    fun projects(): List<Project> =
        (tool("list_projects")["projects"] as? kotlinx.serialization.json.JsonArray)
            ?.map { json.decodeFromJsonElement(Project.serializer(), it) } ?: emptyList()

    fun task(id: String): Pair<TaskItem, List<Comment>> {
        val out = tool("get_task", buildJsonObject { put("id", id) })
        val task = json.decodeFromJsonElement(TaskItem.serializer(), out["task"]!!)
        val comments = (out["comments"] as? kotlinx.serialization.json.JsonArray)
            ?.map { json.decodeFromJsonElement(Comment.serializer(), it) } ?: emptyList()
        return task to comments
    }

    fun createTask(title: String, project: String, due: String) {
        tool("create_tasks", buildJsonObject {
            put("tasks", kotlinx.serialization.json.buildJsonArray {
                add(buildJsonObject {
                    put("title", title)
                    if (project.isNotBlank()) put("project", project)
                    if (due.isNotBlank()) put("due", due)
                })
            })
        })
    }

    fun setTodoDone(todoId: String, done: Boolean) {
        tool("update_todo", buildJsonObject { put("id", todoId); put("done", done) })
    }

    fun comment(taskId: String, body: String) {
        tool("comment", buildJsonObject { put("id", taskId); put("body", body) })
    }

    fun claim(taskId: String) = tool("claim_task", buildJsonObject { put("id", taskId) })

    fun release(taskId: String) = tool("release_task", buildJsonObject { put("id", taskId) })

    fun close(taskId: String) = tool("close_task", buildJsonObject { put("id", taskId) })

    fun logout() {
        try {
            http("/api/logout", "POST", JsonObject(emptyMap()))
        } catch (_: Exception) {
            // Local sign-out matters even if the server can't be reached.
        }
    }
}
