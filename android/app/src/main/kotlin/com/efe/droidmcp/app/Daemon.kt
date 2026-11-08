package com.efe.droidmcp.app

import android.content.Context
import java.net.HttpURLConnection
import java.net.URL

/** What the app knows about the daemon: where it listens and its token. */
object Daemon {
    const val ACTION_STARTED = "com.efe.droidmcp.DAEMON_STARTED"
    const val ACTION_STOPPED = "com.efe.droidmcp.DAEMON_STOPPED"
    const val EXTRA_PORT = "port"
    const val EXTRA_TOKEN = "token"
    const val DEFAULT_PORT = 8765

    private fun prefs(context: Context) = context.getSharedPreferences("daemon", Context.MODE_PRIVATE)

    /** Stores what the daemon's STARTED broadcast carried (app-private storage). */
    fun remember(context: Context, port: Int, token: String) {
        prefs(context).edit().putInt(EXTRA_PORT, port).putString(EXTRA_TOKEN, token).apply()
    }

    fun port(context: Context): Int = prefs(context).getInt(EXTRA_PORT, DEFAULT_PORT)

    /** POSTs /shutdown. Returns null on success, else a short reason. Call off the main thread. */
    fun shutdown(context: Context): String? {
        val token = prefs(context).getString(EXTRA_TOKEN, null) ?: return "token unknown"
        val conn = URL("http://127.0.0.1:${port(context)}/shutdown").openConnection() as HttpURLConnection
        return try {
            conn.requestMethod = "POST"
            conn.connectTimeout = 3000
            conn.readTimeout = 3000
            conn.setRequestProperty("Authorization", "Bearer $token")
            val code = conn.responseCode
            if (code in 200..299) null else "HTTP $code"
        } catch (e: Exception) {
            e.message ?: e.javaClass.simpleName
        } finally {
            conn.disconnect()
        }
    }
}
