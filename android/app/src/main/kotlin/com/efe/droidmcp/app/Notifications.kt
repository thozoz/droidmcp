package com.efe.droidmcp.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent

/** The persistent "daemon running" notification carrying the Stop (kill switch) action. */
object Notifications {
    private const val CHANNEL = "daemon"
    private const val ID = 1

    fun showRunning(context: Context, port: Int) {
        val nm = context.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, "Daemon status", NotificationManager.IMPORTANCE_LOW)
        )
        val stop = PendingIntent.getBroadcast(
            context, 0,
            Intent(context, StopReceiver::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val n = Notification.Builder(context, CHANNEL)
            .setSmallIcon(android.R.drawable.ic_menu_manage)
            .setContentTitle("droidmcp is running")
            .setContentText("An AI agent can control this phone (port $port)")
            .setOngoing(true)
            .addAction(Notification.Action.Builder(null, "Stop", stop).build())
            .build()
        nm.notify(ID, n)
    }

    fun showMessage(context: Context, text: String) {
        val nm = context.getSystemService(NotificationManager::class.java)
        nm.notify(
            ID,
            Notification.Builder(context, CHANNEL)
                .setSmallIcon(android.R.drawable.ic_menu_manage)
                .setContentTitle("droidmcp")
                .setContentText(text)
                .build()
        )
    }

    fun cancel(context: Context) {
        context.getSystemService(NotificationManager::class.java).cancel(ID)
    }
}
