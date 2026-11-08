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
            val script = assets.open("start.sh").use { it.readBytes() }
            val target = File(dir, "start.sh")
            if (target.exists() && target.readBytes().contentEquals(script)) return
            // The daemon's STARTED broadcast launches this process while start.sh is
            // still running; sh reads scripts incrementally, so never rewrite in place.
            val tmp = File(dir, "start.sh.tmp")
            tmp.writeBytes(script)
            if (!tmp.renameTo(target)) error("rename failed")
        }.onFailure { Log.w("droidmcp", "could not write start.sh", it) }
    }

    companion object {
        const val START_COMMAND = "adb shell sh /sdcard/Android/data/com.efe.droidmcp/files/start.sh"
    }
}
