package app.handoff

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/** Persists the workspace host + handoff_session cookie in encrypted prefs. */
class Session(context: Context) {
    // Encrypted prefs restored onto a new device lose their Keystore key and
    // throw on open; fall back to plain prefs (empty session) instead of crashing.
    private val prefs: SharedPreferences = try {
        EncryptedSharedPreferences.create(
            context,
            "handoff_session",
            MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build(),
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    } catch (_: Exception) {
        context.getSharedPreferences("handoff_session", Context.MODE_PRIVATE)
            .also { it.edit().clear().apply() }
    }

    var host: String?
        get() = prefs.getString("host", null)
        set(v) = prefs.edit().putString("host", v).apply()

    var cookie: String?
        get() = prefs.getString("cookie", null)
        set(v) = prefs.edit().putString("cookie", v).apply()

    val api: Api? get() = host?.let { Api(it, cookie) }

    fun clear() = prefs.edit().clear().apply()
}

/** "acme.handoff.app" / "handoff.localhost:8080" / "https://x/" → a base URL we can call. */
fun normalizeHost(input: String): String {
    var h = input.trim().removeSuffix("/")
    if (!h.contains("://")) h = "https://$h"
    return h
}

/** Extracts the token from a pasted /auth/verify?token=… link (or accepts the bare token). */
fun linkToken(input: String): String? {
    val s = input.trim()
    if (s.isEmpty()) return null
    val m = Regex("[?&]token=([^&\\s]+)").find(s)
    if (m != null) return m.groupValues[1]
    return if (s.matches(Regex("[A-Za-z0-9_-]+"))) s else null
}
