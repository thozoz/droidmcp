package com.efe.droidmcp.app

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Daemon lifecycle, sent by the daemon (shell uid) with `am broadcast`. */
class StatusReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            Daemon.ACTION_STARTED -> {
                val port = intent.getIntExtra(Daemon.EXTRA_PORT, Daemon.DEFAULT_PORT)
                intent.getStringExtra(Daemon.EXTRA_TOKEN)?.let { Daemon.remember(context, port, it) }
                Daemon.setClientCount(context, 0)
                Notifications.showRunning(context, port, 0)
            }
            Daemon.ACTION_CLIENTS -> {
                val n = intent.getIntExtra(Daemon.EXTRA_CLIENTS, 0)
                Daemon.setClientCount(context, n)
                Notifications.showRunning(context, Daemon.port(context), n)
            }
            Daemon.ACTION_STOPPED -> Notifications.cancel(context)
        }
    }
}

/** The kill switch: asks the daemon to shut down. */
class StopReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val pending = goAsync()
        Thread {
            try {
                val err = Daemon.shutdown(context)
                // On success the daemon's STOPPED broadcast clears the notification;
                // on failure (e.g. it already died) say so instead of leaving a dead switch.
                if (err != null) Notifications.showMessage(context, "Could not stop: $err")
            } finally {
                pending.finish()
            }
        }.start()
    }
}
