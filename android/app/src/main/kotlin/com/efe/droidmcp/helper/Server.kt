package com.efe.droidmcp.helper

import android.app.UiAutomation
import android.graphics.Bitmap
import android.os.Bundle
import android.os.SystemClock
import android.view.InputDevice
import android.view.InputEvent
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.accessibility.AccessibilityNodeInfo
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.DataOutputStream
import android.graphics.Rect

/**
 * Serves daemon requests one at a time. Each request is
 * {"id","op","args"}; each response is {"id","ok","error"?,"result"?,"binary"?},
 * followed by one binary frame when "binary" is true.
 */
class Server(
    private val ua: UiAutomation,
    private val input: DataInputStream,
    private val output: DataOutputStream,
) {
    /** Nodes of the last tree, indexed by ref "e<n>"; actions resolve refs here. */
    private var nodes: Map<String, AccessibilityNodeInfo> = emptyMap()

    fun run() {
        while (true) {
            val (type, payload) = Frames.read(input) ?: return
            if (type != Frames.JSON) throw IllegalStateException("expected JSON request")
            val req = JSONObject(String(payload, Charsets.UTF_8))
            val id = req.getLong("id")
            val resp = JSONObject().put("id", id)
            var binary: ByteArray? = null
            try {
                val args = req.optJSONObject("args") ?: JSONObject()
                val (result, bin) = handle(req.getString("op"), args)
                resp.put("ok", true)
                if (result != null) resp.put("result", result)
                if (bin != null) {
                    resp.put("binary", true)
                    binary = bin
                }
            } catch (e: Exception) {
                resp.put("ok", false).put("error", e.message ?: e.javaClass.simpleName)
            }
            Frames.write(output, Frames.JSON, resp.toString().toByteArray(Charsets.UTF_8))
            if (binary != null) Frames.write(output, Frames.BINARY, binary)
            output.flush()
        }
    }

    private fun handle(op: String, a: JSONObject): Pair<JSONObject?, ByteArray?> = when (op) {
        "display" -> displayJson() to null
        "screenshot" -> screenshot(a.getInt("max_edge"), a.getString("format"), a.optInt("quality", 70))
        "tree" -> tree(a.optBoolean("interactive_only", true)) to null
        "tap" -> { tap(a.getInt("x"), a.getInt("y")); null to null }
        "swipe" -> { swipe(a.getInt("x1"), a.getInt("y1"), a.getInt("x2"), a.getInt("y2"), a.getLong("ms")); null to null }
        "key" -> { key(a.getString("code")); null to null }
        "click" -> { click(a.getString("ref")); null to null }
        "set_text" -> { appendText(node(a.getString("ref")), a.getString("text")); null to null }
        "type" -> { appendText(focusedInput(), a.getString("text")); null to null }
        else -> throw IllegalArgumentException("unknown op $op")
    }

    private fun displayJson(): JSONObject {
        val (w, h, rot) = Automation.display()
        return JSONObject().put("width", w).put("height", h).put("rotation", rot)
    }

    private fun screenshot(maxEdge: Int, format: String, quality: Int): Pair<JSONObject, ByteArray> {
        val full = ua.takeScreenshot() ?: throw IllegalStateException("screenshot unavailable")
        val display = displayJson().put("width", full.width).put("height", full.height)
        val long = maxOf(full.width, full.height)
        val bmp = if (maxEdge in 1 until long) {
            val s = maxEdge.toDouble() / long
            Bitmap.createScaledBitmap(full, Math.round(full.width * s).toInt(), Math.round(full.height * s).toInt(), true)
        } else {
            full
        }
        val (fmt, mime) = when (format) {
            "webp" -> Bitmap.CompressFormat.WEBP_LOSSY to "image/webp"
            else -> Bitmap.CompressFormat.JPEG to "image/jpeg"
        }
        val out = ByteArrayOutputStream()
        bmp.compress(fmt, quality, out)
        return display.put("mime", mime).put("black", isBlack(bmp)) to out.toByteArray()
    }

    /** True when a 16x16 sample grid is all near-black, which is what FLAG_SECURE produces. */
    private fun isBlack(bmp: Bitmap): Boolean {
        for (gy in 0 until 16) for (gx in 0 until 16) {
            val p = bmp.getPixel((bmp.width - 1) * gx / 15, (bmp.height - 1) * gy / 15)
            if ((p shr 16 and 0xFF) > 8 || (p shr 8 and 0xFF) > 8 || (p and 0xFF) > 8) return false
        }
        return true
    }

    private fun tree(interactiveOnly: Boolean): JSONObject {
        val out = JSONArray()
        val refs = HashMap<String, AccessibilityNodeInfo>()
        // All windows (dialogs, notification shade, IME), not just the focused app.
        val roots = ua.windows.mapNotNull { it.root }.ifEmpty { listOfNotNull(ua.rootInActiveWindow) }
        val rect = Rect()

        fun walk(n: AccessibilityNodeInfo, depth: Int) {
            n.getBoundsInScreen(rect)
            val text = n.text?.toString().orEmpty()
            val desc = n.contentDescription?.toString().orEmpty()
            val keep = !interactiveOnly || (n.isVisibleToUser && !rect.isEmpty &&
                (n.isClickable || n.isLongClickable || n.isScrollable || n.isEditable || n.isFocusable ||
                    text.isNotEmpty() || desc.isNotEmpty()))
            var childDepth = depth
            if (keep) {
                val ref = "e${out.length() + 1}"
                refs[ref] = n
                out.put(
                    JSONObject()
                        .put("ref", ref).put("depth", depth)
                        .put("class", n.className?.toString().orEmpty())
                        .put("text", text).put("desc", desc)
                        .put("id", n.viewIdResourceName.orEmpty())
                        .put("bounds", JSONArray(listOf(rect.left, rect.top, rect.right, rect.bottom)))
                        .put("clickable", n.isClickable).put("long_clickable", n.isLongClickable)
                        .put("focusable", n.isFocusable).put("focused", n.isFocused)
                        .put("scrollable", n.isScrollable).put("editable", n.isEditable)
                )
                childDepth = depth + 1
            }
            for (i in 0 until n.childCount) n.getChild(i)?.let { walk(it, childDepth) }
        }
        roots.forEach { walk(it, 0) }
        nodes = refs
        return JSONObject().put("display", displayJson()).put("nodes", out)
    }

    private fun node(ref: String): AccessibilityNodeInfo {
        val n = nodes[ref] ?: throw IllegalArgumentException("unknown ref $ref")
        if (!n.refresh()) throw IllegalStateException("node $ref is no longer on screen")
        return n
    }

    private fun click(ref: String) {
        if (!node(ref).performAction(AccessibilityNodeInfo.ACTION_CLICK)) {
            throw IllegalStateException("app did not handle ACTION_CLICK on $ref")
        }
    }

    private fun focusedInput(): AccessibilityNodeInfo =
        ua.windows.firstNotNullOfOrNull { it.root?.findFocus(AccessibilityNodeInfo.FOCUS_INPUT) }
            ?: ua.rootInActiveWindow?.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            ?: throw IllegalStateException("no focused text field; pass a ref or tap a field first")

    /** ACTION_SET_TEXT replaces, so keep what is already typed (unless it is only a hint). */
    private fun appendText(n: AccessibilityNodeInfo, text: String) {
        if (!n.isEditable) throw IllegalStateException("node is not an editable text field")
        val current = if (n.isShowingHintText) "" else n.text?.toString().orEmpty()
        val args = Bundle().apply {
            putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, current + text)
        }
        // shortcut: no clipboard+paste fallback yet; fields that refuse SET_TEXT fail here.
        if (!n.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)) {
            throw IllegalStateException("field refused ACTION_SET_TEXT")
        }
    }

    private fun inject(ev: InputEvent) {
        if (!ua.injectInputEvent(ev, true)) {
            throw IllegalStateException(
                "input injection blocked; on MIUI enable 'USB debugging (Security settings)' in developer options"
            )
        }
    }

    private fun touch(action: Int, down: Long, x: Float, y: Float) {
        val ev = MotionEvent.obtain(down, SystemClock.uptimeMillis(), action, x, y, 0)
        ev.source = InputDevice.SOURCE_TOUCHSCREEN
        try {
            inject(ev)
        } finally {
            ev.recycle()
        }
    }

    private fun tap(x: Int, y: Int) {
        val down = SystemClock.uptimeMillis()
        touch(MotionEvent.ACTION_DOWN, down, x.toFloat(), y.toFloat())
        touch(MotionEvent.ACTION_UP, down, x.toFloat(), y.toFloat())
    }

    /** Also long_press when start == end. */
    private fun swipe(x1: Int, y1: Int, x2: Int, y2: Int, ms: Long) {
        val down = SystemClock.uptimeMillis()
        touch(MotionEvent.ACTION_DOWN, down, x1.toFloat(), y1.toFloat())
        val steps = maxOf(1, (ms / 10).toInt())
        for (i in 1..steps) {
            SystemClock.sleep(ms / steps)
            val f = i.toFloat() / steps
            touch(MotionEvent.ACTION_MOVE, down, x1 + (x2 - x1) * f, y1 + (y2 - y1) * f)
        }
        touch(MotionEvent.ACTION_UP, down, x2.toFloat(), y2.toFloat())
    }

    private fun key(name: String) {
        val code = KeyEvent.keyCodeFromString(name)
        if (code == KeyEvent.KEYCODE_UNKNOWN) throw IllegalArgumentException("unknown keycode $name")
        val now = SystemClock.uptimeMillis()
        for (action in intArrayOf(KeyEvent.ACTION_DOWN, KeyEvent.ACTION_UP)) {
            val ev = KeyEvent(now, now, action, code, 0, 0, -1, 0, 0, InputDevice.SOURCE_KEYBOARD)
            inject(ev)
        }
    }
}
