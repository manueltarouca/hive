package xyz.matm.hive.notify

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.RemoteInput
import android.content.Context
import android.content.Intent
import android.graphics.drawable.Icon
import xyz.matm.hive.MainActivity
import xyz.matm.hive.R
import xyz.matm.hive.agents.Agent
import xyz.matm.hive.agents.Notices
import xyz.matm.hive.model.HostConnection

/** Agents' notifications, as the Mac sends them: blocked or done, where it is, what it said. */
object Notifier {
    const val AGENTS = "agents"
    const val SERVICE = "connection"
    const val EXTRA_HOST = "host"
    const val EXTRA_SESSION = "session"
    const val EXTRA_BLOCK = "block"
    const val EXTRA_ANSWER = "answer"
    const val EXTRA_ID = "id"
    const val REPLY = "reply"
    const val EXTRA_MESSAGE = "message"
    const val EXTRA_TOKEN = "token"

    /** Marks intents hive made: the activity is exported, and other apps mustn't steer it to a pane. */
    fun token(ctx: Context): String {
        val prefs = ctx.getSharedPreferences("hive", Context.MODE_PRIVATE)
        return prefs.getString("intentToken", null) ?: java.util.UUID.randomUUID().toString().also { prefs.edit().putString("intentToken", it).apply() }
    }

    fun channels(ctx: Context) {
        val nm = ctx.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(AGENTS, "Agents", NotificationManager.IMPORTANCE_HIGH).apply {
            description = "When an agent needs you or is done"
        })
        nm.createNotificationChannel(NotificationChannel(SERVICE, "Staying connected", NotificationManager.IMPORTANCE_MIN).apply {
            description = "While hive keeps hosts connected in the background"
            setShowBadge(false)
        })
    }

    fun id(host: String, block: String) = (host + block).hashCode()

    fun post(ctx: Context, h: HostConnection, a: Agent) {
        val nid = id(h.id, a.blockID)
        val open = PendingIntent.getActivity(
            ctx, nid,
            Intent(ctx, MainActivity::class.java).putExtra(EXTRA_HOST, h.id).putExtra(EXTRA_SESSION, a.sessionID).putExtra(EXTRA_BLOCK, a.blockID)
                .putExtra(EXTRA_TOKEN, token(ctx)).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val b = Notification.Builder(ctx, AGENTS)
            .setSmallIcon(R.drawable.ic_hive)
            .setContentTitle(Notices.title(a))
            .setSubText(Notices.subtitle(a, h.name, local = false))
            .setContentText(a.message.ifEmpty { a.title })
            .setContentIntent(open)
            .setAutoCancel(true)
            .setCategory(if (a.state == "blocked") Notification.CATEGORY_MESSAGE else Notification.CATEGORY_STATUS)
            .setColor(0xFFF2A93B.toInt())
        if (a.state == "blocked") {
            fun answer(label: String, key: String?, code: Int): Notification.Action.Builder {
                val i = Intent(ctx, AnswerReceiver::class.java).putExtra(EXTRA_HOST, h.id).putExtra(EXTRA_BLOCK, a.blockID)
                    .putExtra(EXTRA_ID, nid).putExtra(EXTRA_SESSION, a.sessionID).putExtra(EXTRA_MESSAGE, a.message)
                if (key != null) i.putExtra(EXTRA_ANSWER, key)
                // A reply needs a mutable intent: the system adds the words.
                val flags = PendingIntent.FLAG_UPDATE_CURRENT or if (key == null) PendingIntent.FLAG_MUTABLE else PendingIntent.FLAG_IMMUTABLE
                return Notification.Action.Builder(Icon.createWithResource(ctx, R.drawable.ic_hive), label,
                    PendingIntent.getBroadcast(ctx, nid * 4 + code, i, flags))
            }
            if (Notices.offersAllow(a)) b.addAction(answer("Allow", "1", 1).build())
            b.addAction(answer("Deny", "escape", 2).build())
            b.addAction(answer("Reply", null, 3).addRemoteInput(RemoteInput.Builder(REPLY).setLabel("Tell ${xyz.matm.hive.agents.Harness.name(a.harness).removeSuffix(" Code")} what to do").build()).build())
        }
        ctx.getSystemService(NotificationManager::class.java).notify(nid, b.build())
    }

    /** After an answer from the notification: gone, or why it couldn't go. */
    fun answered(ctx: Context, nid: Int, ok: Boolean) {
        val nm = ctx.getSystemService(NotificationManager::class.java)
        if (ok) { nm.cancel(nid); return }
        nm.notify(nid, Notification.Builder(ctx, AGENTS).setSmallIcon(R.drawable.ic_hive)
            .setContentTitle("Couldn't answer").setContentText("It isn't waiting anymore, or its host can't be reached. Open hive to look.")
            .setAutoCancel(true).build())
    }
}
