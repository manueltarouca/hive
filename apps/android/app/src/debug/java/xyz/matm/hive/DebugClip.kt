package xyz.matm.hive

import android.content.BroadcastReceiver
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent

/**
 * Debug builds only, for the e2e: sets the clipboard (adb has no way to), or with no text reads
 * it back as the broadcast's result, while the app is in front (Android lets only it read).
 */
class DebugClip : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val clip = ctx.getSystemService(ClipboardManager::class.java)
        val text = intent.getStringExtra("text")
        if (text == null) {
            resultData = clip.primaryClip?.getItemAt(0)?.coerceToText(ctx)?.toString() ?: ""
            return
        }
        clip.setPrimaryClip(ClipData.newPlainText("e2e", text))
    }
}
