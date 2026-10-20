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
    val waiting_on: Member? = null,
    val blocked_by: List<String> = emptyList(),
    val deleted_at: String? = null,
    val triage: String? = null,
    val repeat: String? = null,
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

@Serializable
data class Notification(
    val id: Long,
    val kind: String,
    val task_id: String = "",
    val task_title: String = "",
    val actor: Member? = null,
    val excerpt: String? = null,
    val read: Boolean = false,
    val at: String = "",
)

@Serializable
private data class RedeemOut(val token: String, val member: Member? = null)

/** One task the quick-add parser produced; serializes to create_tasks' NewTask. */
data class NewTask(
    val title: String,
    val due: String? = null,
    val assignee: String? = null,
    val project: String? = null,
    val priority: Int? = null,
    val repeat: String? = null,
)

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

    fun searchTasks(query: String = "", projectId: String? = null): List<TaskItem> =
        tasks("tasks", tool("search_tasks", buildJsonObject {
            if (query.isNotEmpty()) put("query", query)
            if (projectId != null) put("project", projectId)
            put("limit", 500)
        }))

    /** The "My tasks" feed: open tasks assigned to me, recent first. */
    fun myTasks(): List<TaskItem> =
        tasks("tasks", tool("search_tasks", buildJsonObject {
            put("assignee", "me"); put("sort", "recent"); put("limit", 500)
        }))

    fun projects(): List<Project> =
        (tool("list_projects")["projects"] as? kotlinx.serialization.json.JsonArray)
            ?.map { json.decodeFromJsonElement(Project.serializer(), it) } ?: emptyList()

    fun createProject(name: String) {
        tool("create_project", buildJsonObject { put("name", name); put("visibility", "company") })
    }

    fun members(): List<Member> =
        (tool("list_members")["members"] as? kotlinx.serialization.json.JsonArray)
            ?.map { json.decodeFromJsonElement(Member.serializer(), it) } ?: emptyList()

    fun inbox(): Pair<List<Notification>, Int> {
        val out = tool("inbox", buildJsonObject { put("limit", 200) })
        val items = (out["notifications"] as? kotlinx.serialization.json.JsonArray)
            ?.map { json.decodeFromJsonElement(Notification.serializer(), it) } ?: emptyList()
        val unread = out["unread"]?.toString()?.toIntOrNull() ?: items.count { !it.read }
        return items to unread
    }

    fun markRead(ids: List<Long>? = null, task: String? = null, all: Boolean = false) {
        tool("mark_read", buildJsonObject {
            if (all) put("all", true)
            if (task != null) put("task", task)
            if (ids != null) put("ids", kotlinx.serialization.json.buildJsonArray { ids.forEach { add(kotlinx.serialization.json.JsonPrimitive(it)) } })
        })
    }

    /** Swaps a "Pair a device" code for a session token (POST /api/app/redeem). */
    fun redeemCode(code: String): Member {
        val out = http("/api/app/redeem", "POST", buildJsonObject { put("code", code) })
        val r = json.decodeFromJsonElement(RedeemOut.serializer(), json.parseToJsonElement(out).jsonObject)
        cookie = r.token
        return r.member ?: Member(id = "", name = "")
    }

    fun task(id: String): Pair<TaskItem, List<Comment>> {
        val out = tool("get_task", buildJsonObject { put("id", id) })
        val task = json.decodeFromJsonElement(TaskItem.serializer(), out["task"]!!)
        val comments = (out["comments"] as? kotlinx.serialization.json.JsonArray)
            ?.map { json.decodeFromJsonElement(Comment.serializer(), it) } ?: emptyList()
        return task to comments
    }

    fun createTasks(tasks: List<NewTask>) {
        tool("create_tasks", buildJsonObject {
            put("tasks", kotlinx.serialization.json.buildJsonArray {
                tasks.forEach { t ->
                    add(buildJsonObject {
                        put("title", t.title)
                        t.due?.let { put("due", it) }
                        t.assignee?.let { put("assignee", it) }
                        t.project?.let { put("project", it) }
                        t.priority?.let { put("priority", it) }
                        t.repeat?.let { put("repeat", it) }
                    })
                }
            })
        })
    }

    fun reopenTask(id: String) {
        tool("update_task", buildJsonObject { put("id", id); put("status", "open") })
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
