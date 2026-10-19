package xyz.matm.hive.notify

import android.app.Notification
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder
import xyz.matm.hive.HiveApp
import xyz.matm.hive.MainActivity
import xyz.matm.hive.R

/**
 * Keeps the connected hosts' attach streams open while the app is in the
 * background, so their agents can notify. While the phone is awake: in
 * Doze the network goes, and on wake hive reconnects and notifies what
 * changed meanwhile.
 */
class ConnectionService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val model = (application as HiveApp).model
        val n = model.hosts.count { it.connected || it.host.always }
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val note = Notification.Builder(this, Notifier.SERVICE)
            .setSmallIcon(R.drawable.ic_hive)
            .setContentTitle(if (n == 1) "Connected to 1 host" else "Connected to $n hosts")
            .setContentText("So agents can reach you")
            .setContentIntent(open)
            .setOngoing(true)
            .build()
        // Foreground first, always: a start must be answered with startForeground, even one a stop follows at once.
        startForeground(1, note, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        when (intent?.action) {
            STOP -> { stopForeground(STOP_FOREGROUND_REMOVE); stopSelf() }
            ANSWER -> {
                val host = intent.getStringExtra(Notifier.EXTRA_HOST)
                val block = intent.getStringExtra(Notifier.EXTRA_BLOCK)
                val nid = intent.getIntExtra(Notifier.EXTRA_ID, 0)
                if (host != null && block != null) {
                    model.answerRemotely(host, block, intent.getStringExtra(Notifier.EXTRA_ANSWER), intent.getStringExtra(Notifier.REPLY),
                        intent.getStringExtra(Notifier.EXTRA_MESSAGE) ?: "") { ok ->
                        Notifier.answered(this, nid, ok)
                        // Started only to answer, with the app away and nothing to keep: done.
                        if (!started && !model.foreground && model.hosts.none { it.connected && it.host.always }) {
                            stopForeground(STOP_FOREGROUND_REMOVE); stopSelf()
                        }
                    }
                }
            }
        }
        return START_NOT_STICKY
    }

    companion object {
        private const val STOP = "xyz.matm.hive.STOP"
        private const val ANSWER = "xyz.matm.hive.ANSWER"
        @Volatile private var started = false

        fun start(ctx: Context) {
            runCatching { ctx.startForegroundService(Intent(ctx, ConnectionService::class.java)) }.onSuccess { started = true }
        }

        /** An answer from a notification (allowed to start the service: the user just touched it). */
        fun answer(ctx: Context, from: Intent) {
            val i = Intent(ctx, ConnectionService::class.java).setAction(ANSWER)
            from.extras?.let(i::putExtras)
            runCatching { ctx.startForegroundService(i) }
        }

        /** Through the service, after its startForeground: stopping it before that would crash the app. */
        fun stop(ctx: Context) {
            if (!started) return
            started = false
            runCatching { ctx.startService(Intent(ctx, ConnectionService::class.java).setAction(STOP)) }
        }
    }
}
