package com.efe.droidmcp.app

import android.content.Context
import android.net.ConnectivityManager
import java.net.Inet4Address

/**
 * The "setup prompt": text the user pastes into an AI agent (e.g. Claude Code)
 * so the agent connects itself to this phone. Built from the current state:
 * over USB the bridge handles port and token; with LAN access the prompt
 * carries this phone's Wi-Fi address and the token.
 */
object SetupPrompt {
    private const val REPO = "github.com/thozoz/droidmcp"

    /** Returns the prompt and whether it contains the token. */
    fun build(context: Context): Pair<String, Boolean> {
        val ip = wifiIPv4(context)
        val token = Daemon.token(context)
        return if (Daemon.lan(context) && ip != null && token != null) {
            lan(ip, Daemon.port(context), token) to true
        } else {
            usb() to false
        }
    }

    private fun usb() = """
        Set up droidmcp, an MCP server that lets you see and control my Android phone, and connect yourself to it. The phone is plugged into this computer with USB debugging on.

        1. Run `adb devices`. If no device is listed, stop and ask me to plug in the phone and allow USB debugging.
        2. Start the server on the phone: `adb shell sh /sdcard/Android/data/com.efe.droidmcp/files/start.sh`
        3. If `droidmcp-bridge` is not on PATH, install it: `go install $REPO/daemon/cmd/droidmcp-bridge@latest`. It forwards the port and reads the token over adb, so no secret is needed here.
        4. Register it as an MCP server named "android":
           - Claude Code: `claude mcp add android -- droidmcp-bridge`
           - Other clients: add {"mcpServers":{"android":{"command":"droidmcp-bridge"}}} to their MCP config.
        5. Tell me to restart or reload you so the tools load. Then call the `screenshot` tool to confirm it works.
    """.trimIndent()

    private fun lan(ip: String, port: Int, token: String): String {
        val url = "http://$ip:$port/mcp"
        return """
            Set up droidmcp, an MCP server that lets you see and control my Android phone, and connect yourself to it. The phone serves MCP (Streamable HTTP) on my local network at $url and needs this bearer token: $token

            1. Register it as an MCP server named "android":
               - Claude Code: `claude mcp add --transport http android $url --header "Authorization: Bearer $token"`
               - Other clients: add {"mcpServers":{"android":{"type":"http","url":"$url","headers":{"Authorization":"Bearer $token"}}}} to their MCP config.
            2. Tell me to restart or reload you so the tools load. Then call the `screenshot` tool to confirm it works.

            The token gives full control of the phone: do not repeat it back, log it or store it anywhere else. This computer must be on the same Wi-Fi as the phone. If the server is unreachable, ask me to check that droidmcp is running with LAN access enabled.
        """.trimIndent()
    }

    /** This phone's IPv4 address on the active network (Wi-Fi), or null. */
    private fun wifiIPv4(context: Context): String? {
        val cm = context.getSystemService(ConnectivityManager::class.java)
        val props = cm.getLinkProperties(cm.activeNetwork) ?: return null
        return props.linkAddresses.map { it.address }
            .firstOrNull { it is Inet4Address && !it.isLoopbackAddress }?.hostAddress
    }
}
