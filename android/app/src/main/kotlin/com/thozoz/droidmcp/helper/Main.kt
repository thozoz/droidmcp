package com.thozoz.droidmcp.helper

import android.net.LocalSocket
import android.net.LocalSocketAddress
import android.os.Looper
import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.io.DataInputStream
import java.io.DataOutputStream

/**
 * Entry point for `app_process -Djava.class.path=<base.apk> /system/bin
 * com.thozoz.droidmcp.helper.Main <args>`, run as the shell user.
 *
 *   serve <name>  connect to the daemon's abstract socket @name and serve requests
 *   probe         print what UiAutomation can see, for manual checks
 */
object Main {
    @JvmStatic
    fun main(args: Array<String>) {
        // AccessibilityInteractionClient builds Handlers on the main looper; app_process has none.
        Looper.prepareMainLooper()
        when (args.firstOrNull()) {
            "serve" -> serve(args.getOrNull(1) ?: usage())
            "probe" -> probe()
            else -> usage()
        }
        System.exit(0) // process death releases the UiAutomation connection
    }

    private fun usage(): Nothing {
        System.err.println("usage: Main serve <socket-name> | probe")
        System.exit(2)
        throw IllegalStateException()
    }

    private fun serve(name: String) {
        val ua = Automation.connect()
        val sock = LocalSocket()
        sock.connect(LocalSocketAddress(name, LocalSocketAddress.Namespace.ABSTRACT))
        // Returns when the daemon closes the socket; the helper never outlives it.
        Server(
            ua,
            DataInputStream(BufferedInputStream(sock.inputStream)),
            DataOutputStream(BufferedOutputStream(sock.outputStream)),
        ).run()
    }

    private fun probe() {
        val ua = Automation.connect()
        val bmp = ua.takeScreenshot()
        println("screenshot: ${bmp?.width}x${bmp?.height}")
        println("display: ${Automation.display()}")
        println("windows: ${ua.windows.map { it.root?.packageName }}")
    }
}
