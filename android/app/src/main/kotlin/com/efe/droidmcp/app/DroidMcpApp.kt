package com.efe.droidmcp.app

import android.app.Application
import android.util.Log
import java.io.File

class DroidMcpApp : Application() {
    override fun onCreate() {
        super.onCreate()
        writeStartScript()
    }

    /**
     * Copies assets/start.sh to the app's external files dir, where `adb shell`
     * can run it. Rewritten on every start so it matches the installed APK.
     */
    private fun writeStartScript() {
        val dir = getExternalFilesDir(null) ?: return
        runCatching {
            assets.open("start.sh").use { input ->
                File(dir, "start.sh").outputStream().use { input.copyTo(it) }
            }
        }.onFailure { Log.w("droidmcp", "could not write start.sh", it) }
    }

    companion object {
        const val START_COMMAND = "adb shell sh /sdcard/Android/data/com.efe.droidmcp/files/start.sh"
    }
}
