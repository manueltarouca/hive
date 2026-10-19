package xyz.matm.hive.notify

import android.app.RemoteInput
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * Allow, Deny and Reply on a notification. The answer may need a connection
 * first, longer than a receiver may live: the connection service does it.
 */
class AnswerReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val text = RemoteInput.getResultsFromIntent(intent)?.getCharSequence(Notifier.REPLY)?.toString()
        if (intent.getStringExtra(Notifier.EXTRA_ANSWER) == null && text.isNullOrBlank()) return
        ConnectionService.answer(ctx, Intent(intent).putExtra(Notifier.REPLY, text))
    }
}
