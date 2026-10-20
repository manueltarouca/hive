package xyz.matm.hive.ui

import android.content.Intent
import android.provider.Settings as SystemSettings
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.ui.semantics.heading
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import xyz.matm.hive.model.AppModel
import xyz.matm.hive.model.Host
import java.util.Locale

/** Settings, as the Mac's where they apply to a phone: appearance, the terminal, agents, about. Hosts have their own screen. */
@Composable
fun Settings(model: AppModel, close: () -> Unit) {
    val p = LocalPalette.current
    val ctx = LocalContext.current
    Column(Modifier.fillMaxSize().background(p.bg).statusBarsPadding().navigationBarsPadding()) {
        Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.clip(RoundedCornerShape(50)).clickable(role = Role.Button, onClick = close).semantics { contentDescription = "Back" }.padding(8.dp)) {
                LineIcon(Icon.Back, p.muted, 20.dp)
            }
            Spacer(Modifier.width(6.dp))
            BasicText("Settings", style = Type.title.copy(color = p.text))
        }
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = 16.dp)) {
            Section("Appearance") {
                Item("Theme") {
                    Choice(listOf("system" to "System", "night" to "Night", "day" to "Day"), model.themeChoice, model::chooseTheme)
                }
                Item("Text size", "When a pane fits the phone. Pinch to zoom a pane another device sizes.") {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        RoundKey(Icon.Minus, "Smaller text") { model.chooseTextSize(model.textSp - 0.5f) }
                        Spacer(Modifier.width(10.dp))
                        BasicText(String.format(Locale.ROOT, "%.1f", model.textSp), style = Type.label.copy(color = p.text))
                        Spacer(Modifier.width(10.dp))
                        RoundKey(Icon.Plus, "Larger text") { model.chooseTextSize(model.textSp + 0.5f) }
                        Spacer(Modifier.width(14.dp))
                        // A line as the terminal shows it, at that size.
                        BasicText("❯ ls ~/code", style = Type.mono.copy(fontFamily = LocalMono.current, fontSize = model.textSp.sp, color = p.muted), maxLines = 1)
                    }
                }
            }
            Section("Terminal") {
                Toggle("Ask before pasting several lines", "When the program didn't ask for pasted text, each line may run as a command.",
                    model.confirmPaste, model::chooseConfirmPaste)
                Toggle("Open panes fitted to the phone", "A pane opens at the phone's size, not zoomed out to another device's. The button at the top gives the size back.",
                    model.openFitted, model::chooseOpenFitted)
            }
            Section("Agents") {
                Item("Notifications", "When an agent needs you or is done.") {
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        for ((v, label) in listOf("background" to "When hive is in the background", "always" to "Always", "never" to "Never")) {
                            Radio(label, model.notify == v) { model.chooseNotify(v) }
                        }
                    }
                }
                Link("Notification settings on this phone") {
                    runCatching { ctx.startActivity(Intent(SystemSettings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(SystemSettings.EXTRA_APP_PACKAGE, ctx.packageName)) }
                }
            }
            Section("About") {
                val version = runCatching { ctx.packageManager.getPackageInfo(ctx.packageName, 0).versionName }.getOrNull() ?: ""
                Item("hive for Android") { BasicText(version, style = Type.small.copy(color = p.muted)) }
                for (h in model.hosts.filter { it.connected }) {
                    Item(h.name) { BasicText(h.client?.serverVersion ?: "", style = Type.small.copy(color = p.muted, fontFamily = FontFamily.Monospace)) }
                }
                BasicText("Fonts: JetBrains Mono, Inter and Noto Sans Symbols (SIL Open Font License). libghostty-vt (MIT), ZXing and CameraX (Apache 2.0). Program logos from Simple Icons (CC0) and their makers.",
                    style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(vertical = 8.dp))
            }
            Spacer(Modifier.height(24.dp))
        }
    }
}

@Composable
private fun Section(title: String, content: @Composable () -> Unit) {
    val p = LocalPalette.current
    BasicText(title, style = Type.small.copy(color = p.muted), modifier = Modifier.padding(top = 18.dp, bottom = 8.dp, start = 4.dp).semantics { heading() })
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(p.surface).border(1.dp, p.line, RoundedCornerShape(12.dp))
        .padding(horizontal = 14.dp, vertical = 6.dp)) { content() }
}

@Composable
private fun Item(title: String, help: String? = null, control: @Composable () -> Unit) {
    val p = LocalPalette.current
    Column(Modifier.fillMaxWidth().padding(vertical = 10.dp)) {
        BasicText(title, style = Type.body.copy(color = p.text))
        if (help != null) BasicText(help, style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(top = 2.dp))
        Spacer(Modifier.height(8.dp))
        control()
    }
}

/** A segmented choice: one of a few words. */
@Composable
private fun Choice(options: List<Pair<String, String>>, value: String, set: (String) -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        for ((v, label) in options) Pill(label, selected = v == value) { set(v) }
    }
}

@Composable
private fun RoundKey(icon: Icon, description: String, onClick: () -> Unit) {
    val p = LocalPalette.current
    Box(Modifier.size(36.dp).clip(CircleShape).background(p.raised).clickable(role = Role.Button, onClick = onClick)
        .semantics { contentDescription = description }, contentAlignment = Alignment.Center) { LineIcon(icon, p.text, 16.dp) }
}

@Composable
private fun Radio(label: String, on: Boolean, set: () -> Unit) {
    val p = LocalPalette.current
    Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).selectable(selected = on, role = Role.RadioButton, onClick = set)
        .padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(20.dp).clip(CircleShape).border(1.5.dp, if (on) p.honey else p.muted, CircleShape), contentAlignment = Alignment.Center) {
            if (on) Box(Modifier.size(10.dp).clip(CircleShape).background(p.honey))
        }
        Spacer(Modifier.width(12.dp))
        BasicText(label, style = Type.body.copy(color = p.text))
    }
}

@Composable
private fun Toggle(title: String, help: String, on: Boolean, set: (Boolean) -> Unit) {
    val p = LocalPalette.current
    // One switch for the row: the track inside only shows it.
    Row(Modifier.fillMaxWidth().toggleable(value = on, role = Role.Switch, onValueChange = set).padding(vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            BasicText(title, style = Type.body.copy(color = p.text))
            BasicText(help, style = Type.tiny.copy(color = p.muted), modifier = Modifier.padding(top = 2.dp))
        }
        Spacer(Modifier.width(12.dp))
        Track(on)
    }
}

/** An on/off switch in hive's colours (honey when on), with a 48 dp target. */
@Composable
private fun Switch(on: Boolean, description: String, set: (Boolean) -> Unit) {
    Box(Modifier.size(56.dp, 48.dp).toggleable(value = on, role = Role.Switch, onValueChange = set)
        .semantics { contentDescription = description }, contentAlignment = Alignment.Center) { Track(on) }
}

/** The switch's look alone: honey when on; off keeps an outline, or a pale track on a pale card would vanish in Day. */
@Composable
private fun Track(on: Boolean) {
    val p = LocalPalette.current
    Box(Modifier.size(width = 44.dp, height = 26.dp).clip(RoundedCornerShape(50)).background(if (on) p.honey else p.raised)
        .then(if (on) Modifier else Modifier.border(1.dp, p.muted.copy(alpha = 0.6f), RoundedCornerShape(50)))
        .padding(3.dp), contentAlignment = if (on) Alignment.CenterEnd else Alignment.CenterStart) {
        Box(Modifier.size(20.dp).clip(CircleShape).background(if (on) p.onHoney else p.muted))
    }
}

@Composable
private fun Link(text: String, onClick: () -> Unit) {
    val p = LocalPalette.current
    Row(Modifier.fillMaxWidth().clickable(role = Role.Button, onClick = onClick).padding(vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        BasicText(text, style = Type.body.copy(color = p.honey))
    }
}
