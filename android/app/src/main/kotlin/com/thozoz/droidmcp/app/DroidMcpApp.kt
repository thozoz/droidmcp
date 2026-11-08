package com.thozoz.droidmcp.app

import android.app.Application
import android.util.Log
import java.io.File

class DroidMcpApp : Application() {
    override fun onCreate() {
        super.onCreate()
        writeStartScript()
        Thread(::extractBridges, "droidmcp-bridges").start()
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

    /**
     * Copies the computer-side bridge binaries to files/bridge/, where an agent
     * fetches the one for its OS with `adb pull`. About 40 MB, so only when the
     * APK was installed or updated since the last copy.
     */
    private fun extractBridges() {
        val dir = File(getExternalFilesDir(null) ?: return, "bridge")
        val stamp = File(dir, ".installed")
        val version = packageManager.getPackageInfo(packageName, 0).lastUpdateTime.toString()
        if (stamp.exists() && stamp.readText() == version) return
        runCatching {
            dir.mkdirs()
            for (name in assets.list("bridge").orEmpty()) {
                val tmp = File(dir, "$name.tmp")
                assets.open("bridge/$name").use { input -> tmp.outputStream().use { input.copyTo(it) } }
                if (!tmp.renameTo(File(dir, name))) error("rename $name failed")
            }
            stamp.writeText(version)
        }.onFailure { Log.w("droidmcp", "could not extract bridges", it) }
    }

    companion object {
        const val START_COMMAND = "adb shell sh /sdcard/Android/data/com.thozoz.droidmcp/files/start.sh"
        const val BRIDGE_DIR = "/sdcard/Android/data/com.thozoz.droidmcp/files/bridge"
    }
}
