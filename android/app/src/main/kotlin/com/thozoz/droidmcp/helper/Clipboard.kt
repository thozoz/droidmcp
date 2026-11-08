package com.thozoz.droidmcp.helper

import android.content.ClipData
import android.os.IBinder

/**
 * Accesses Android's clipboard from shell uid (2000) via the hidden IClipboard binder service.
 * Context.getSystemService(CLIPBOARD_SERVICE) fails under shell uid because ContextImpl
 * passes package name "android" which does not belong to uid 2000.
 */
object Clipboard {
    private const val SHELL_PKG = "com.android.shell"

    private val iClipboard: Any? by lazy {
        try {
            val smCls = Class.forName("android.os.ServiceManager")
            val binder = smCls.getDeclaredMethod("getService", String::class.java)
                .invoke(null, "clipboard") as? IBinder ?: return@lazy null
            val stubCls = Class.forName("android.content.IClipboard\$Stub")
            stubCls.getDeclaredMethod("asInterface", IBinder::class.java).invoke(null, binder)
        } catch (_: Throwable) {
            null
        }
    }

    /**
     * Reads the current primary clip if allowed.
     * Note: On Android 10+, background processes cannot read the clipboard unless
     * they are the default IME or hold the active window. Under shell uid this
     * returns null; in that case previous clipboard content cannot be restored.
     */
    fun getPrimaryClip(): ClipData? {
        val iclip = iClipboard ?: return null
        for (m in iclip.javaClass.methods) {
            if (m.name == "getPrimaryClip") {
                try {
                    val args = when (m.parameterCount) {
                        4 -> arrayOf(SHELL_PKG, null, 0, 0) // Android 14+ (pkg, tag, userId, deviceId)
                        3 -> arrayOf(SHELL_PKG, 0, 0)
                        2 -> arrayOf(SHELL_PKG, 0)
                        1 -> arrayOf(SHELL_PKG)
                        else -> continue
                    }
                    val clip = m.invoke(iclip, *args) as? ClipData
                    if (clip != null) return clip
                } catch (_: Throwable) {}
            }
        }
        return null
    }

    /**
     * Sets the primary clip. Shell uid has permission to write to the clipboard across Android versions.
     */
    fun setPrimaryClip(clip: ClipData): Boolean {
        val iclip = iClipboard ?: return false
        for (m in iclip.javaClass.methods) {
            if (m.name == "setPrimaryClip") {
                try {
                    val args = when (m.parameterCount) {
                        5 -> arrayOf(clip, SHELL_PKG, null, 0, 0) // Android 14+ (clip, pkg, tag, userId, deviceId)
                        4 -> arrayOf(clip, SHELL_PKG, null, 0)
                        3 -> arrayOf(clip, SHELL_PKG, 0)
                        2 -> arrayOf(clip, SHELL_PKG)
                        1 -> arrayOf(clip)
                        else -> continue
                    }
                    m.invoke(iclip, *args)
                    return true
                } catch (_: Throwable) {}
            }
        }
        return false
    }
}
