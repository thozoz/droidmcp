package com.efe.droidmcp.helper

import android.accessibilityservice.AccessibilityServiceInfo
import android.app.UiAutomation
import android.os.HandlerThread
import android.os.Looper

/**
 * Builds a connected [UiAutomation] outside of instrumentation, the same way
 * the `uiautomator` shell command does. The constructor and connect(int) are
 * hidden APIs; app_process (shell uid) is not subject to hidden-API checks.
 * Only one UiAutomation may be connected system-wide at a time.
 */
object Automation {
    fun connect(): UiAutomation {
        val thread = HandlerThread("droidmcp-uiautomation").apply { start() }
        val connection = Class.forName("android.app.UiAutomationConnection")
            .getDeclaredConstructor().newInstance()
        val iface = Class.forName("android.app.IUiAutomationConnection")
        val ctor = UiAutomation::class.java.getDeclaredConstructor(Looper::class.java, iface)
        ctor.isAccessible = true
        val ua = ctor.newInstance(thread.looper, connection)
        UiAutomation::class.java
            .getDeclaredMethod("connect", Int::class.javaPrimitiveType)
            .invoke(ua, UiAutomation.FLAG_DONT_SUPPRESS_ACCESSIBILITY_SERVICES)
        // getWindows() is empty unless the service asks for interactive windows.
        ua.serviceInfo = ua.serviceInfo.apply {
            flags = flags or AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS or
                AccessibilityServiceInfo.FLAG_REPORT_VIEW_IDS
        }
        return ua
    }

    /** Logical size (already rotated) and rotation of display 0, via the hidden DisplayManagerGlobal. */
    fun display(): Triple<Int, Int, Int> {
        val dmg = Class.forName("android.hardware.display.DisplayManagerGlobal")
        val global = dmg.getDeclaredMethod("getInstance").invoke(null)
        val info = dmg.getDeclaredMethod("getDisplayInfo", Int::class.javaPrimitiveType).invoke(global, 0)
        val cls = info.javaClass
        return Triple(
            cls.getField("logicalWidth").getInt(info),
            cls.getField("logicalHeight").getInt(info),
            cls.getField("rotation").getInt(info),
        )
    }
}
