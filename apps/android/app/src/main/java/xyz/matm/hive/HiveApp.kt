package xyz.matm.hive

import android.app.Application
import xyz.matm.hive.model.AppModel

class HiveApp : Application() {
    lateinit var model: AppModel
        private set

    override fun onCreate() {
        super.onCreate()
        xyz.matm.hive.notify.Notifier.channels(this)
        model = AppModel(this)
        model.onNotify = { h, a -> xyz.matm.hive.notify.Notifier.post(this, h, a) }
        model.launch()
    }
}
