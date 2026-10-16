package app.civu.civu_mobile

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel

/** Reads app metadata only; never initializes the map SDK or returns its key. */
class MapConfiguration(context: Context, messenger: BinaryMessenger) {
    private val appContext = context.applicationContext
    private val channel = MethodChannel(messenger, "birdtie/map_configuration")

    init {
        channel.setMethodCallHandler { call, result ->
            if (call.method != "hasAndroidAMapKey") {
                result.notImplemented()
            } else {
                try {
                    val info = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                        appContext.packageManager.getApplicationInfo(
                            appContext.packageName,
                            PackageManager.ApplicationInfoFlags.of(PackageManager.GET_META_DATA.toLong())
                        )
                    } else {
                        @Suppress("DEPRECATION")
                        appContext.packageManager.getApplicationInfo(
                            appContext.packageName, PackageManager.GET_META_DATA
                        )
                    }
                    val key = info.metaData?.getString("com.amap.api.v2.apikey")?.trim()
                    result.success(!key.isNullOrEmpty() && !key.startsWith("\${"))
                } catch (_: Exception) {
                    result.error("MAP_CONFIGURATION_UNAVAILABLE", "Map configuration could not be read.", null)
                }
            }
        }
    }

    fun close() {
        channel.setMethodCallHandler(null)
    }
}
