package xyz.matm.hive

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import xyz.matm.hive.model.HostsCode
import xyz.matm.hive.model.Offered
import xyz.matm.hive.ui.AddHost
import xyz.matm.hive.ui.Desk
import xyz.matm.hive.ui.HiveTheme
import xyz.matm.hive.ui.Offer
import xyz.matm.hive.ui.Switcher

/** Where the app is: the desk (the session), the switcher, adding hosts. */
sealed class Screen {
    object Desk : Screen()
    object Switcher : Screen()
    object AddHost : Screen()
    class Offer(val hosts: List<Offered>) : Screen()
    object Scan : Screen()
    object Settings : Screen()
    object Hosts : Screen()
}

class MainActivity : ComponentActivity() {
    private var screen by mutableStateOf<Screen>(Screen.Desk)
    /** Where the scanner goes back to: the screen it was opened from. */
    private var scanBack: Screen = Screen.AddHost
    private val model get() = (application as HiveApp).model

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        if (model.hosts.isEmpty()) screen = Screen.AddHost
        handle(intent)
        setContent {
            HiveTheme(model.themeChoice) {
                // The status and navigation bars follow hive's theme, not only the system's.
                val night = xyz.matm.hive.ui.LocalPalette.current.night
                androidx.compose.runtime.SideEffect {
                    androidx.core.view.WindowCompat.getInsetsController(window, window.decorView).apply {
                        isAppearanceLightStatusBars = !night
                        isAppearanceLightNavigationBars = !night
                    }
                }
                when (val s = screen) {
                    Screen.Desk -> Desk(model, openSwitcher = { screen = if (model.hosts.isEmpty()) Screen.AddHost else Screen.Switcher }, openLink = ::open)
                    Screen.Switcher -> {
                        BackHandler { screen = Screen.Desk }
                        Switcher(model, close = { screen = Screen.Desk }, addHost = { screen = Screen.AddHost }, settings = { screen = Screen.Settings },
                            hosts = { screen = Screen.Hosts })
                    }
                    Screen.Settings -> {
                        BackHandler { screen = Screen.Switcher }
                        xyz.matm.hive.ui.Settings(model, close = { screen = Screen.Switcher })
                    }
                    Screen.Hosts -> {
                        BackHandler { screen = Screen.Switcher }
                        xyz.matm.hive.ui.HostsScreen(model, close = { screen = Screen.Switcher }, addHost = { screen = Screen.AddHost },
                            scan = { scanBack = Screen.Hosts; screen = Screen.Scan })
                    }
                    Screen.AddHost -> {
                        BackHandler(model.hosts.isNotEmpty()) { screen = Screen.Switcher }
                        AddHost(model, close = { screen = if (model.hosts.isEmpty()) Screen.AddHost else Screen.Switcher },
                            scan = { scanBack = Screen.AddHost; screen = Screen.Scan }, offer = { screen = Screen.Offer(it) }, done = { screen = Screen.Desk })
                    }
                    is Screen.Offer -> {
                        BackHandler { screen = Screen.AddHost }
                        Offer(model, s.hosts, close = { screen = Screen.AddHost }, done = { screen = Screen.Switcher })
                    }
                    Screen.Scan -> {
                        BackHandler { screen = scanBack }
                        xyz.matm.hive.ui.Scanner(close = { screen = scanBack }) { code ->
                            screen = HostsCode.decode(code)?.let { Screen.Offer(it) } ?: scanBack
                        }
                    }
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handle(intent)
    }

    /** A hosts code opened as a link goes through the same review as a scanned one. */
    private fun handle(intent: Intent?) {
        val host = intent?.getStringExtra(xyz.matm.hive.notify.Notifier.EXTRA_HOST)
        val session = intent?.getStringExtra(xyz.matm.hive.notify.Notifier.EXTRA_SESSION)
        if (host != null && session != null && intent.getStringExtra(xyz.matm.hive.notify.Notifier.EXTRA_TOKEN) == xyz.matm.hive.notify.Notifier.token(this)) {
            model.hosts.firstOrNull { it.id == host }?.let { model.select(it, session, block = intent.getStringExtra(xyz.matm.hive.notify.Notifier.EXTRA_BLOCK)) }
            screen = Screen.Desk
            return
        }
        val data = intent?.dataString ?: return
        HostsCode.decode(data)?.let { screen = Screen.Offer(it) }
    }

    private fun open(uri: String) {
        val u = Uri.parse(uri)
        if (u.scheme !in setOf("http", "https", "mailto")) return
        runCatching { startActivity(Intent(Intent.ACTION_VIEW, u)) }
    }

    private val askNotify = registerForActivityResult(androidx.activity.result.contract.ActivityResultContracts.RequestPermission()) {}

    override fun onResume() {
        super.onResume()
        model.resumed()
        xyz.matm.hive.notify.ConnectionService.stop(this)
        // Agents notify: asked once there's a host to hear from.
        if (model.hosts.isNotEmpty() && model.notify != "never" && android.os.Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED &&
            !getSharedPreferences("hive", MODE_PRIVATE).getBoolean("askedNotify", false)) {
            getSharedPreferences("hive", MODE_PRIVATE).edit().putBoolean("askedNotify", true).apply()
            askNotify.launch(android.Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    override fun onPause() {
        super.onPause()
        model.paused()
        // Still visible here, so the service may start: it keeps hosts set to stay connected (and only
        // those) running in the background, for their agents' notifications. On-demand hosts let go.
        if (model.hosts.any { it.host.always && it.link != xyz.matm.hive.model.Link.Idle }) xyz.matm.hive.notify.ConnectionService.start(this)
    }
}
