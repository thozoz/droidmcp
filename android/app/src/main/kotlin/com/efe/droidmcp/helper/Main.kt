package com.efe.droidmcp.helper

import android.os.Looper

/**
 * Entry point for `app_process -Djava.class.path=<base.apk> /system/bin
 * com.efe.droidmcp.helper.Main <args>`, run as the shell user.
 */
object Main {
    @JvmStatic
    fun main(args: Array<String>) {
        // AccessibilityInteractionClient builds Handlers on the main looper; app_process has none.
        Looper.prepareMainLooper()
        when (args.firstOrNull()) {
            "probe" -> probe()
            else -> {
                System.err.println("usage: Main probe")
                System.exit(2)
            }
        }
    }

    private fun probe() {
        val ua = Automation.connect()
        val bmp = ua.takeScreenshot()
        println("screenshot: ${bmp?.width}x${bmp?.height}")
        println("windows: ${ua.windows.size}")
        println("root: ${ua.rootInActiveWindow?.packageName}")
        System.exit(0) // process death releases the UiAutomation connection
    }
}
