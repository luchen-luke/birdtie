package app.civu.civu_mobile

import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ApplicationInfo
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.util.Log
import android.view.InputDevice
import android.view.MotionEvent
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

/** Opt-in own-window diagnostic; never OS input injection or a domain action. */
internal class NativeUiAcceptance(private val activity: Activity, engine: FlutterEngine) : AutoCloseable {
    private val channel = MethodChannel(engine.dartExecutor.binaryMessenger, "app.birdtie/qa/ui")
    private val handler = Handler(Looper.getMainLooper())
    private var generation = 0
    private var active = false
    private var downTime = 0L
    private var lastX = 0f
    private var lastY = 0f
    private val allowed: Boolean get() = BuildConfig.BUILD_TYPE == "profile" &&
        (activity.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE) != 0 &&
        (activity.packageName.endsWith(".birdtiepreview") || activity.packageName.endsWith(".birdtieacceptance"))

    // Fixed read-only observation endpoint. Shell must hold DUMP; ordinary
    // applications cannot invoke it. Unlike starting the Activity, this does
    // not pause/resume the map, cancel touches or accept arbitrary arguments.
    private val observationReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            if (intent?.action != OBSERVE_MAP || !allowed || active || !activity.hasWindowFocus()) return
            channel.invokeMethod("mapObserve60", null, object : MethodChannel.Result {
                override fun success(result: Any?) {
                    Log.i("BirdtieUiQA", "event=observe_broadcast accepted=${result == true}")
                }
                override fun error(code: String, message: String?, details: Any?) { Log.i("BirdtieUiQA", "event=unavailable") }
                override fun notImplemented() { Log.i("BirdtieUiQA", "event=unavailable") }
            })
        }
    }
    private var observationRegistered = false

    init {
        if (allowed) {
            val filter = IntentFilter(OBSERVE_MAP)
            if (Build.VERSION.SDK_INT >= 33) {
                activity.registerReceiver(observationReceiver, filter, "android.permission.DUMP", handler, Context.RECEIVER_EXPORTED)
            } else {
                @Suppress("DEPRECATION")
                activity.registerReceiver(observationReceiver, filter, "android.permission.DUMP", handler)
            }
            observationRegistered = true
        }
    }

    fun armFrom(intent: Intent?) {
        val action = intent?.getStringExtra("birdtie.qa.uiAction") ?: return
        intent.removeExtra("birdtie.qa.uiAction")
        if (!allowed || action !in setOf("keyboardShow", "keyboardHide", "mapPan", "mapObserve60")) return
        // Observation must never emit even a cancellation touch of its own.
        if (action == "mapObserve60" && active) return
        cancel()
        val serial = generation
        // Dart additionally checks the current Now route and unobstructed map
        // state. No coordinates, text or business selector are accepted.
        handler.postDelayed({
            if (serial != generation || !activity.hasWindowFocus()) return@postDelayed
            channel.invokeMethod(action, null, object : MethodChannel.Result {
                override fun success(result: Any?) {
                    if (result == true && action == "mapPan" && serial == generation) pan(serial)
                }
                override fun error(code: String, message: String?, details: Any?) { Log.i("BirdtieUiQA", "event=unavailable") }
                override fun notImplemented() { Log.i("BirdtieUiQA", "event=unavailable") }
            })
        }, 180)
    }

    private fun pan(serial: Int) {
        if (!activity.hasWindowFocus()) return
        val root = activity.window.decorView
        if (root.width < 100 || root.height < 100) return
        downTime = SystemClock.uptimeMillis()
        active = true
        val startX = root.width * .25f
        val endX = root.width * .65f
        val y = root.height * .35f
        Log.i("BirdtieUiQA", "event=pan_start durationMs=6000 scope=OWN_WINDOW_SYNTHETIC")
        send(MotionEvent.ACTION_DOWN, startX, y)
        fun step() {
            if (!active || serial != generation || !activity.hasWindowFocus()) { cancel(); return }
            channel.invokeMethod("mapPanContinue", null, object : MethodChannel.Result {
                override fun success(result: Any?) {
                    if (result != true || !active || serial != generation || !activity.hasWindowFocus()) { cancel(); return }
                    val elapsed = SystemClock.uptimeMillis() - downTime
                    val fraction = (elapsed / 6000f).coerceIn(0f, 1f)
                    send(if (fraction >= 1f) MotionEvent.ACTION_UP else MotionEvent.ACTION_MOVE,
                        startX + (endX - startX) * fraction, y)
                    if (fraction >= 1f) {
                        active = false
                        Log.i("BirdtieUiQA", "event=pan_end scope=OWN_WINDOW_SYNTHETIC")
                    } else handler.postDelayed({ step() }, 32)
                }
                override fun error(code: String, message: String?, details: Any?) { cancel() }
                override fun notImplemented() { cancel() }
            })
        }
        handler.postDelayed({ step() }, 32)
    }

    private fun send(action: Int, x: Float, y: Float) {
        lastX = x; lastY = y
        val event = MotionEvent.obtain(downTime, SystemClock.uptimeMillis(), action, x, y, 0)
        event.source = InputDevice.SOURCE_TOUCHSCREEN
        try { activity.dispatchTouchEvent(event) } finally { event.recycle() }
    }

    fun cancel() {
        generation++
        handler.removeCallbacksAndMessages(null)
        if (active) send(MotionEvent.ACTION_CANCEL, lastX, lastY)
        active = false
    }
    override fun close() {
        if (observationRegistered) {
            activity.unregisterReceiver(observationReceiver)
            observationRegistered = false
        }
        cancel()
    }

    private companion object { const val OBSERVE_MAP = "app.birdtie.qa.OBSERVE_MAP" }
}
