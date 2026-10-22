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
    const val EXTRA_NOTE = "note"
    const val EXTRA_ACTION = "action"
    const val EXTRA_ARGS = "args"
    const val SCRIPTS = "scripts"

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
        nm.createNotificationChannel(NotificationChannel(SCRIPTS, "Scripts", NotificationManager.IMPORTANCE_HIGH).apply {
            description = "What your hive scripts tell you"
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
            .setContentText(Notices.text(a))
            .setStyle(Notification.BigTextStyle().bigText(Notices.text(a)))
            .setContentIntent(open)
            .setAutoCancel(true)
            .setCategory(if (a.state == "blocked") Notification.CATEGORY_MESSAGE else Notification.CATEGORY_STATUS)
            // Stopped is coral, as in the app: it won't go on by itself.
            .setColor(if (a.shown == "stopped") 0xFFCC6666.toInt() else 0xFFF2A93B.toInt())
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

    fun scriptID(host: String, note: String) = ("script" + host + note).hashCode()

    /** A script's notification: its buttons run the script's actions, and the body says what each does. */
    fun postScript(ctx: Context, h: HostConnection, n: xyz.matm.hive.model.ScriptNote, session: String?) {
        val nid = scriptID(h.id, n.id)
        val open = PendingIntent.getActivity(
            ctx, nid,
            Intent(ctx, MainActivity::class.java).putExtra(EXTRA_HOST, h.id).putExtra(EXTRA_SESSION, n.session).putExtra(EXTRA_BLOCK, n.block)
                .putExtra(EXTRA_TOKEN, token(ctx)).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val b = Notification.Builder(ctx, SCRIPTS)
            .setSmallIcon(R.drawable.ic_hive)
            .setContentTitle(n.title)
            .setSubText(session ?: h.name)
            .setContentText(n.oneLine)
            .setStyle(Notification.BigTextStyle().bigText(n.bodyWithSteps))
            .setContentIntent(open)
            .setAutoCancel(true)
            .setColor(0xFFF2A93B.toInt())
        n.actions.take(3).forEachIndexed { i, a ->
            val intent = Intent(ctx, ActionReceiver::class.java).putExtra(EXTRA_HOST, h.id).putExtra(EXTRA_NOTE, n.id)
                .putExtra(EXTRA_ID, nid).putExtra(EXTRA_ACTION, a.action).putExtra(EXTRA_ARGS, a.args).putExtra(EXTRA_BLOCK, n.block)
            b.addAction(Notification.Action.Builder(Icon.createWithResource(ctx, R.drawable.ic_hive), a.title,
                PendingIntent.getBroadcast(ctx, nid * 4 + i, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)).build())
        }
        ctx.getSystemService(NotificationManager::class.java).notify(nid, b.build())
    }

    /** More notifications than are worth showing one by one, from while the phone was away. */
    fun postMore(ctx: Context, h: HostConnection, more: Int) {
        ctx.getSystemService(NotificationManager::class.java).notify(("more" + h.id).hashCode(), Notification.Builder(ctx, SCRIPTS)
            .setSmallIcon(R.drawable.ic_hive).setContentTitle("$more more from scripts").setContentText("On ${h.name}, while hive was away.")
            .setAutoCancel(true).build())
    }

    fun cancelScript(ctx: Context, host: String, note: String) =
        ctx.getSystemService(NotificationManager::class.java).cancel(scriptID(host, note))

    /** After a script notification's button: gone, or why nothing was sent. */
    fun actionDone(ctx: Context, nid: Int, err: String?) {
        val nm = ctx.getSystemService(NotificationManager::class.java)
        if (err == null) { nm.cancel(nid); return }
        nm.notify(nid, Notification.Builder(ctx, SCRIPTS).setSmallIcon(R.drawable.ic_hive)
            .setContentTitle("Nothing was sent").setContentText(err).setAutoCancel(true).build())
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
