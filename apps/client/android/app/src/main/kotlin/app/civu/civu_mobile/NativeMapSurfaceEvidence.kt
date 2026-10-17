package app.civu.civu_mobile

import android.app.Activity
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.graphics.Bitmap
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.util.Log
import android.view.PixelCopy
import android.view.SurfaceView
import android.view.TextureView
import android.view.View
import android.view.ViewGroup
import io.flutter.embedding.engine.FlutterEngine
import kotlin.math.max

/** Explicitly armed profile-only source-buffer diagnostics. No pixels leave RAM.
 * These observations are SOURCE_ONLY: a valid source is not proof of screen
 * presentation, and a spatially flat source is not automatically a blank map.
 */
internal class NativeMapSurfaceEvidence(
    private val activity: Activity,
    private val engine: FlutterEngine,
) : AutoCloseable {
    private val handler = Handler(Looper.getMainLooper())
    private var generation = 0
    private var active = false
    private var started = 0L
    private var samples = 0
    private var pending = false
    private var lastMap: View? = null
    private var lastBuffer: View? = null
    private var sourceSequence = 0
    private val tick = Runnable { sample() }
    private val enabled: Boolean
        get() = BuildConfig.BUILD_TYPE == "profile" &&
            (activity.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE) != 0 &&
            (activity.packageName.endsWith(".birdtiepreview") ||
                activity.packageName.endsWith(".birdtieacceptance"))

    fun armFrom(intent: Intent?) {
        if (!enabled || intent?.getBooleanExtra("birdtie.qa.mapSurfaceEvidence", false) != true) return
        // Consume the launch flag so rotation does not silently re-arm a sample.
        intent.removeExtra("birdtie.qa.mapSurfaceEvidence")
        stop("rearm")
        active = true
        started = SystemClock.elapsedRealtime()
        samples = 0
        lastMap = null
        lastBuffer = null
        sourceSequence = 0
        Log.i(TAG, "event=start limitMs=8000 sampleLimit=80 scope=SOURCE_ONLY")
        handler.post(tick)
    }

    fun stop(reason: String) {
        if (active) Log.i(TAG, "event=end reason=$reason samples=$samples elapsedMs=${SystemClock.elapsedRealtime() - started} scope=SOURCE_ONLY")
        active = false
        generation++
        handler.removeCallbacks(tick)
        // A PixelCopy callback owns its Bitmap until it completes.
    }

    private fun sample() {
        if (!active || pending) return
        val elapsed = SystemClock.elapsedRealtime() - started
        if (elapsed >= 8000 || samples >= 80) { stop("bounded_complete"); return }
        samples++
        var source: View? = null
        var provider = "unknown"
        // VirtualDisplay roots are not necessarily in the Activity decor tree.
        // Only the engine's own platform views may supply a map source.
        for (id in 0..255) {
            val root = engine.platformViewsControllerDelegator.getPlatformViewById(id) ?: continue
            val map = findMap(root) ?: continue
            provider = if (isMapbox(map)) "mapbox" else "amap"
            source = findBuffer(map)
            if (source != null) {
                if (lastMap !== map || lastBuffer !== source) {
                    sourceSequence++
                    lastMap = map
                    lastBuffer = source
                }
                break
            }
        }
        if (source == null) {
            Log.i(TAG, "event=sample n=$samples elapsedMs=$elapsed status=NOT_FOUND provider=$provider")
            schedule()
            return
        }
        val attached = source.isAttachedToWindow
        val width = source.width
        val height = source.height
        val valid = when (source) {
            is TextureView -> source.isAvailable
            is SurfaceView -> source.holder.surface.isValid
            else -> false
        }
        val kind = if (source is TextureView) "texture" else "surface"
        val prefix = "event=sample n=$samples elapsedMs=$elapsed provider=$provider kind=$kind sourceSeq=$sourceSequence attached=$attached valid=$valid width=$width height=$height"
        if (!attached || !valid || width <= 0 || height <= 0) {
            Log.i(TAG, "$prefix status=SOURCE_UNAVAILABLE")
            schedule()
            return
        }
        val bitmap = Bitmap.createBitmap(64, 64, Bitmap.Config.ARGB_8888)
        bitmap.eraseColor(SENTINEL)
        val began = SystemClock.elapsedRealtimeNanos()
        val gen = generation
        pending = true
        if (source is TextureView) {
            try {
                source.getBitmap(bitmap)
                complete(bitmap, gen, prefix, "TEXTURE_SNAPSHOT_ATTEMPTED", began, true)
            } catch (_: RuntimeException) {
                complete(bitmap, gen, prefix, "TEXTURE_ERROR", began, false)
            }
        } else if (source is SurfaceView && Build.VERSION.SDK_INT >= 24) {
            try {
                PixelCopy.request(source, bitmap, { status ->
                    complete(bitmap, gen, prefix, "PIXEL_COPY_$status", began, status == PixelCopy.SUCCESS)
                }, handler)
            } catch (_: RuntimeException) {
                complete(bitmap, gen, prefix, "PIXEL_COPY_REQUEST_ERROR", began, false)
            }
        } else {
            complete(bitmap, gen, prefix, "UNSUPPORTED", began, false)
        }
    }

    private fun complete(bitmap: Bitmap, gen: Int, prefix: String, status: String, began: Long, copied: Boolean) {
        pending = false
        if (active && gen == generation) {
            var metrics = ""
            if (copied) {
                val pixels = IntArray(64 * 64)
                bitmap.getPixels(pixels, 0, 64, 0, 0, 64, 64)
                var sentinel = 0
                var opaque = 0
                var sum = 0.0
                var sumSquared = 0.0
                val bins = IntArray(4096)
                var largest = 0
                for (pixel in pixels) {
                    if (pixel == SENTINEL) sentinel++
                    if ((pixel ushr 24) >= 250) opaque++
                    val red = (pixel ushr 16) and 255
                    val green = (pixel ushr 8) and 255
                    val blue = pixel and 255
                    val luma = (red * 54 + green * 183 + blue * 19) / 256.0
                    sum += luma
                    sumSquared += luma * luma
                    val bin = ((red ushr 4) shl 8) or ((green ushr 4) shl 4) or (blue ushr 4)
                    bins[bin]++
                    largest = max(largest, bins[bin])
                }
                metrics = if (sentinel == pixels.size) " content=UNKNOWN_UNCHANGED" else {
                    val variance = max(0.0, sumSquared / pixels.size - (sum / pixels.size) * (sum / pixels.size))
                    " content=OBSERVED opaquePermille=${opaque * 1000 / pixels.size} flatPermille=${largest * 1000 / pixels.size} lumaVarianceMilli=${(variance * 1000).toLong()}"
                }
                pixels.fill(0)
            }
            Log.i(TAG, "$prefix status=$status$metrics durationUs=${(SystemClock.elapsedRealtimeNanos() - began) / 1000} scope=SOURCE_ONLY")
        }
        bitmap.eraseColor(0)
        bitmap.recycle()
        schedule()
    }

    private fun schedule() {
        if (active) handler.postDelayed(tick, 100)
    }

    private fun isMapbox(view: View): Boolean {
        var type: Class<*>? = view.javaClass
        while (type != null) {
            if (type.name == "com.mapbox.maps.MapView") return true
            type = type.superclass
        }
        return false
    }

    private fun findMap(view: View): View? {
        if (isMapbox(view) || view.javaClass.name == "com.amap.api.maps.MapView") return view
        if (view is ViewGroup) for (i in 0 until view.childCount) findMap(view.getChildAt(i))?.let { return it }
        return null
    }

    private fun findBuffer(view: View): View? {
        if (view is TextureView || view is SurfaceView) return view
        if (view is ViewGroup) for (i in 0 until view.childCount) findBuffer(view.getChildAt(i))?.let { return it }
        return null
    }

    override fun close() {
        stop("engine_cleanup")
        lastMap = null
        lastBuffer = null
    }

    companion object {
        private const val TAG = "BirdtieSurface"
        private const val SENTINEL = 0x7f12ab34
    }
}
