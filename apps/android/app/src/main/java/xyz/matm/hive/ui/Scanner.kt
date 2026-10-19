package xyz.matm.hive.ui

import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicText
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import xyz.matm.hive.model.HostsCode
import java.util.concurrent.Executors

/** The code scanner: the camera, looking for the Mac's hosts code. Anything else is ignored. */
@Composable
fun Scanner(close: () -> Unit, found: (String) -> Unit) {
    val p = LocalPalette.current
    val ctx = LocalContext.current
    val owner = LocalLifecycleOwner.current
    var allowed by remember { mutableStateOf(ContextCompat.checkSelfPermission(ctx, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) }
    var denied by remember { mutableStateOf(false) }
    var otherCode by remember { mutableStateOf(false) }
    var noCamera by remember { mutableStateOf(false) }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { allowed = it; denied = !it }
    LaunchedEffect(Unit) { if (!allowed) ask.launch(Manifest.permission.CAMERA) }
    val analysis = remember { Executors.newSingleThreadExecutor() }
    DisposableEffect(Unit) { onDispose { analysis.shutdown() } }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        if (allowed) {
            AndroidView(factory = { c ->
                // A TextureView: a SurfaceView sits behind the window, under Compose's background.
                val view = PreviewView(c).apply { implementationMode = PreviewView.ImplementationMode.COMPATIBLE }
                val future = ProcessCameraProvider.getInstance(c)
                future.addListener({
                    val provider = future.get()
                    val preview = Preview.Builder().build().also { it.surfaceProvider = view.surfaceProvider }
                    var done = false
                    val reader = ImageAnalysis.Builder().setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
                    reader.setAnalyzer(analysis) { image ->
                        val plane = image.planes[0]
                        val buf = plane.buffer
                        val y = ByteArray(buf.remaining()).also { buf.get(it) }
                        val text = if (done) null else Qr.decode(y, image.width, image.height, plane.rowStride)
                        image.close()
                        if (text != null) {
                            if (HostsCode.decode(text) != null) {
                                done = true
                                view.post { provider.unbindAll(); found(text) }
                            } else view.post { otherCode = true }
                        }
                    }
                    // The back camera, else whichever there is (emulators and some tablets have one).
                    val selector = if (runCatching { provider.hasCamera(CameraSelector.DEFAULT_BACK_CAMERA) }.getOrDefault(false)) CameraSelector.DEFAULT_BACK_CAMERA
                    else provider.availableCameraInfos.firstOrNull()?.cameraSelector
                    if (selector == null) { noCamera = true; return@addListener }
                    runCatching {
                        provider.unbindAll()
                        provider.bindToLifecycle(owner, selector, preview, reader)
                    }.onFailure { noCamera = true }
                }, ContextCompat.getMainExecutor(c))
                view
            }, modifier = Modifier.fillMaxSize())
        }
        Column(Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding()) {
            Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.clip(RoundedCornerShape(50)).background(Color.Black.copy(alpha = 0.4f)).clickable(role = Role.Button, onClick = close)
                    .semantics { contentDescription = "Back" }.padding(8.dp)) { LineIcon(Icon.Back, Color.White, 20.dp) }
                Spacer(Modifier.width(10.dp))
                BasicText("Scan the code", style = Type.title.copy(color = Color.White))
            }
            Box(Modifier.weight(1f).fillMaxWidth(), contentAlignment = Alignment.Center) {
                // Where to hold the code.
                Box(Modifier.size(240.dp).border(2.dp, p.honey, RoundedCornerShape(16.dp)))
            }
            val note = when {
                noCamera -> "No camera to scan with. Copy the code on your Mac and use Paste it."
                denied -> "hive needs the camera to read the code. Or copy it on your Mac and use Paste it."
                otherCode -> "That code isn't from hive. On your Mac: Hive menu, Share Hosts with Phone."
                else -> "On your Mac: Hive menu, Share Hosts with Phone."
            }
            BasicText(note, style = Type.body.copy(color = Color.White), modifier = Modifier.padding(24.dp))
            Spacer(Modifier.height(12.dp))
        }
    }
}
