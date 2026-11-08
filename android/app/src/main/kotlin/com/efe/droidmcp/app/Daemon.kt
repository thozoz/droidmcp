package com.efe.droidmcp.app

import android.content.Context
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

/** What the app knows about the daemon: where it listens, its token, and start options. */
object Daemon {
    const val ACTION_STARTED = "com.efe.droidmcp.DAEMON_STARTED"
    const val ACTION_STOPPED = "com.efe.droidmcp.DAEMON_STOPPED"
    const val EXTRA_PORT = "port"
    const val EXTRA_TOKEN = "token"
    const val DEFAULT_PORT = 8765

    private const val KEY_LAN = "lan"
    private const val KEY_SHELL = "shell"

    private fun prefs(context: Context) = context.getSharedPreferences("daemon", Context.MODE_PRIVATE)

    /** Stores what the daemon's STARTED broadcast carried (app-private storage). */
    fun remember(context: Context, port: Int, token: String) {
        prefs(context).edit().putInt(EXTRA_PORT, port).putString(EXTRA_TOKEN, token).apply()
    }

    fun port(context: Context): Int = prefs(context).getInt(EXTRA_PORT, DEFAULT_PORT)
    fun token(context: Context): String? = prefs(context).getString(EXTRA_TOKEN, null)

    fun lan(context: Context) = prefs(context).getBoolean(KEY_LAN, false)
    fun shell(context: Context) = prefs(context).getBoolean(KEY_SHELL, false)

    /**
     * Saves start options and writes them as daemon flags next to start.sh,
     * which reads them on the next start. Only these known flags are written.
     */
    fun setOptions(context: Context, lan: Boolean, shell: Boolean) {
        prefs(context).edit().putBoolean(KEY_LAN, lan).putBoolean(KEY_SHELL, shell).apply()
        val flags = buildList {
            if (lan) add("-addr 0.0.0.0:$DEFAULT_PORT")
            if (shell) add("-enable-shell")
        }.joinToString(" ")
        val dir = context.getExternalFilesDir(null) ?: return
        File(dir, "flags").writeText(flags + "\n")
    }

    /** True when the daemon answers /health with our token. Call off the main thread. */
    fun running(context: Context): Boolean = request(context, "GET", "/health")?.first == 200

    /** POSTs /shutdown. Returns null on success, else a short reason. Call off the main thread. */
    fun shutdown(context: Context): String? {
        val (code, _) = request(context, "POST", "/shutdown") ?: return "daemon not reachable"
        return if (code in 200..299) null else "HTTP $code"
    }

    /** Asks the daemon for a new token and stores it. Returns it, or null. Call off the main thread. */
    fun rotateToken(context: Context): String? {
        val (code, body) = request(context, "POST", "/token") ?: return null
        if (code != 200) return null
        val token = JSONObject(body).getString("token")
        remember(context, port(context), token)
        return token
    }

    private fun request(context: Context, method: String, path: String): Pair<Int, String>? {
        val token = token(context) ?: return null
        val conn = URL("http://127.0.0.1:${port(context)}$path").openConnection() as HttpURLConnection
        return try {
            conn.requestMethod = method
            conn.connectTimeout = 3000
            conn.readTimeout = 3000
            conn.setRequestProperty("Authorization", "Bearer $token")
            val code = conn.responseCode
            val body = (if (code < 400) conn.inputStream else conn.errorStream)
                ?.bufferedReader()?.use { it.readText() }.orEmpty()
            code to body
        } catch (e: Exception) {
            null
        } finally {
            conn.disconnect()
        }
    }
}
