package com.efe.droidmcp.app

import android.Manifest
import android.app.Activity
import android.app.AlertDialog
import android.content.ClipData
import android.content.ClipboardManager
import android.content.pm.PackageManager
import android.os.Bundle
import android.widget.Button
import android.widget.Switch
import android.widget.TextView
import android.widget.Toast
import com.efe.droidmcp.R
import java.util.concurrent.Executors

class MainActivity : Activity() {
    private val io = Executors.newSingleThreadExecutor()
    private var revealed = false
    private var running = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        findViewById<TextView>(R.id.command).text = DroidMcpApp.START_COMMAND
        findViewById<Button>(R.id.copyCommand).setOnClickListener { copy("start command", DroidMcpApp.START_COMMAND) }
        findViewById<Button>(R.id.revealToken).setOnClickListener {
            revealed = !revealed
            render()
        }
        findViewById<Button>(R.id.copyToken).setOnClickListener {
            Daemon.token(this)?.let { copy("droidmcp token", it) }
        }
        findViewById<Button>(R.id.rotateToken).setOnClickListener { rotate() }
        findViewById<Button>(R.id.stop).setOnClickListener { stop() }

        val lan = findViewById<Switch>(R.id.lan)
        val shell = findViewById<Switch>(R.id.shell)
        lan.isChecked = Daemon.lan(this)
        shell.isChecked = Daemon.shell(this)
        lan.setOnCheckedChangeListener { _, on ->
            if (on) confirmLan(lan) else saveOptions()
        }
        shell.setOnCheckedChangeListener { _, _ -> saveOptions() }

        if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 0)
        }
    }

    override fun onResume() {
        super.onResume()
        refresh()
    }

    override fun onDestroy() {
        io.shutdown()
        super.onDestroy()
    }

    private fun refresh() {
        io.execute {
            val up = Daemon.running(this)
            runOnUiThread {
                running = up
                render()
            }
        }
    }

    private fun render() {
        val status = findViewById<TextView>(R.id.status)
        status.text = if (running) "● running :${Daemon.port(this)}" else "○ stopped"
        status.setTextColor(getColor(if (running) R.color.ok else R.color.muted))

        val token = Daemon.token(this)
        findViewById<TextView>(R.id.token).text = when {
            token == null -> "Start the daemon to get a token."
            revealed -> token
            else -> "•".repeat(12) + token.takeLast(4)
        }
        findViewById<Button>(R.id.revealToken).text = if (revealed) "Hide" else "Show"
        findViewById<Button>(R.id.rotateToken).isEnabled = running
        findViewById<Button>(R.id.stop).isEnabled = running
    }

    private fun confirmLan(lan: Switch) {
        AlertDialog.Builder(this)
            .setTitle("Allow LAN access?")
            .setMessage(
                "The daemon will accept connections from every device on your network. " +
                    "Traffic is plain HTTP, so anyone on the same network can read the token " +
                    "and take full control of this phone. Use only on a network you trust."
            )
            .setPositiveButton("Allow") { _, _ -> saveOptions() }
            .setNegativeButton("Cancel") { _, _ -> lan.isChecked = false }
            .setOnCancelListener { lan.isChecked = false }
            .show()
    }

    private fun saveOptions() {
        val lan = findViewById<Switch>(R.id.lan).isChecked
        val shell = findViewById<Switch>(R.id.shell).isChecked
        io.execute { Daemon.setOptions(this, lan, shell) }
        if (running) toast("Applies when the daemon is next started")
    }

    private fun rotate() {
        io.execute {
            val token = Daemon.rotateToken(this)
            runOnUiThread {
                toast(if (token != null) "New token issued; the old one no longer works" else "Could not reach the daemon")
                refresh()
            }
        }
    }

    private fun stop() {
        io.execute {
            val err = Daemon.shutdown(this)
            runOnUiThread {
                if (err != null) toast("Could not stop: $err") else toast("Stopping")
                refresh()
            }
        }
    }

    private fun copy(label: String, text: String) {
        getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText(label, text))
        toast("Copied")
    }

    private fun toast(msg: String) = Toast.makeText(this, msg, Toast.LENGTH_SHORT).show()
}
