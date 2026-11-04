package xyz.matm.hive.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import xyz.matm.hive.model.AppModel

/** Find in a pane: a field over the pane's top, the count, previous and next, and Done. Back closes it. */
@Composable
fun FindBar(model: AppModel, f: AppModel.FindUi) {
    val p = LocalPalette.current
    val focus = remember { FocusRequester() }
    LaunchedEffect(f) { focus.requestFocus() }
    BackHandler { model.endFind() }
    Row(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(p.surface).border(1.dp, p.honey.copy(alpha = 0.6f), RoundedCornerShape(14.dp))
            .padding(start = 12.dp, end = 4.dp).heightIn(min = 48.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        LineIcon(Icon.Search, p.muted, 18.dp)
        Spacer(Modifier.width(8.dp))
        Box(Modifier.weight(1f)) {
            if (f.query.isEmpty()) BasicText("Find in this pane", style = Type.body.copy(color = p.muted))
            BasicTextField(
                f.query, { model.updateFind(it) }, singleLine = true, textStyle = Type.body.copy(color = p.text), cursorBrush = SolidColor(p.honey),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { model.stepFind(-1) }),
                modifier = Modifier.fillMaxWidth().focusRequester(focus).semantics { contentDescription = "Find in this pane" },
            )
        }
        val count = when {
            f.query.isEmpty() -> ""
            f.count == 0 -> "No matches"
            else -> "${f.current + 1} of ${f.count}"
        }
        if (count.isNotEmpty()) BasicText(count, style = Type.small.copy(color = p.muted), modifier = Modifier.padding(horizontal = 6.dp))
        StepKey(up = true) { model.stepFind(-1) }
        StepKey(up = false) { model.stepFind(1) }
        Box(Modifier.clip(RoundedCornerShape(50)).clickable(role = Role.Button) { model.endFind() }.padding(horizontal = 12.dp, vertical = 12.dp)) {
            BasicText("Done", style = Type.label.copy(color = p.honey))
        }
    }
}

@Composable
private fun StepKey(up: Boolean, onClick: () -> Unit) {
    val p = LocalPalette.current
    Box(
        Modifier.size(40.dp).clip(CircleShape).clickable(role = Role.Button, onClick = onClick)
            .semantics { contentDescription = if (up) "Previous match" else "Next match" },
        contentAlignment = Alignment.Center,
    ) { LineIcon(Icon.Chevron, p.muted, 18.dp, Modifier.graphicsLayer { rotationZ = if (up) 180f else 0f }) }
}
