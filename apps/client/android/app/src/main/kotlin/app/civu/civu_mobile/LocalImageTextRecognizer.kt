package app.civu.civu_mobile

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import com.googlecode.tesseract.android.TessBaseAPI
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/** Only explicit local image text checking. No URLs, gallery, HTTP, logs or writes of image/text. */
class LocalImageTextRecognizer(private val context: Context, messenger: BinaryMessenger) : MethodChannel.MethodCallHandler {
    private val channel = MethodChannel(messenger, "birdtie/local_image_text")
    private val main = Handler(Looper.getMainLooper())
    private val closed = AtomicBoolean(false)
    private class Job(val owner: LocalImageTextRecognizer, val id: String) {
        val cancelled = AtomicBoolean(false)
        val deadline = SystemClock.elapsedRealtime() + 30000L
    }
    companion object {
        // Shared across Activity/engine replacement. An old worker must actually end first.
        private val active = AtomicReference<Job?>(null)
        private val worker = Executors.newSingleThreadExecutor { task ->
            Thread(task, "birdtie-local-text").apply { isDaemon = true }
        }
        private const val modelCommit = "87416418657359cb625c412a48b6e1d6d41c29bd"
        private val models = mapOf(
            "eng" to "7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2",
            "chi_sim" to "a5fcb6f0db1e1d6d8522f39db4e848f05984669172e584e8d76b6b3141e1f730"
        )
        private fun digest(bytes: ByteArray) = MessageDigest.getInstance("SHA-256").digest(bytes)
            .joinToString("") { "%02x".format(it.toInt() and 255) }
    }
    init { channel.setMethodCallHandler(this) }
    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "cancel" -> {
                val id = call.argument<String>("requestId")
                active.get()?.let { if (it.owner === this && it.id == id) it.cancelled.set(true) }
                result.success(null)
            }
            "recognize" -> start(call, result)
            else -> result.notImplemented()
        }
    }
    private fun start(call: MethodCall, result: MethodChannel.Result) {
        val args = call.arguments as? Map<*, *>
        val bytes = args?.get("bytes") as? ByteArray
        val id = args?.get("requestId") as? String
        val hash = args?.get("sha256") as? String
        val width = args?.get("width") as? Int
        val height = args?.get("height") as? Int
        if (closed.get() || args?.size != 5 || bytes == null || id == null || hash == null ||
            width == null || height == null || !id.matches(Regex("local-text-[0-9]{1,18}")) ||
            !hash.matches(Regex("[0-9a-f]{64}")) || bytes.isEmpty() || bytes.size > 10 * 1024 * 1024 ||
            width !in 1..8192 || height !in 1..8192 || width.toLong() * height > 16L * 1024 * 1024) {
            result.error("LIMIT", "Local input invalid", null); return
        }
        val job = Job(this, id)
        if (!active.compareAndSet(null, job)) { result.error("BUSY", "Previous local work not ended", null); return }
        worker.execute {
            var bitmap: Bitmap? = null
            var api: TessBaseAPI? = null
            var reply: Any? = null
            var failure: String? = null
            try {
                check(job)
                if (digest(bytes) != hash) throw IllegalArgumentException()
                val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
                BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
                if (bounds.outWidth != width || bounds.outHeight != height ||
                    bounds.outMimeType !in listOf("image/png", "image/jpeg")) throw IllegalArgumentException()
                var sample = 1
                fun scaled(n: Int) = (n + sample - 1) / sample
                while (scaled(width) > 1600 || scaled(height) > 1600 || scaled(width).toLong() * scaled(height) > 1000000L) sample *= 2
                val targetW = scaled(width); val targetH = scaled(height)
                bitmap = BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply {
                    inSampleSize = sample; inPreferredConfig = Bitmap.Config.ARGB_8888
                }) ?: throw IllegalArgumentException()
                // PNG/JPEG decoders may round sampling differently. Normalize to the closed Dart algorithm.
                if (bitmap.width != targetW || bitmap.height != targetH) {
                    val resized = Bitmap.createScaledBitmap(bitmap, targetW, targetH, true)
                    bitmap.recycle(); bitmap = resized
                }
                check(job)
                val dataPath = installModels(job)
                val tess = TessBaseAPI { _ ->
                    // All Tess methods, including stop, stay on the recognition worker.
                    // stop is cooperative during HOCR recognition, not an init/codec hard deadline.
                    if (job.cancelled.get() || closed.get() || SystemClock.elapsedRealtime() >= job.deadline) api?.stop()
                }
                api = tess
                if (!tess.init(dataPath, "chi_sim+eng", TessBaseAPI.OEM_LSTM_ONLY)) throw IllegalStateException()
                check(job)
                tess.setPageSegMode(TessBaseAPI.PageSegMode.PSM_AUTO)
                tess.setImage(bitmap)
                // getUTF8Text alone is not stoppable. HOCR performs recognition first;
                // its markup is immediately discarded and never logged, stored or returned.
                tess.getHOCRText(0)
                check(job)
                val lines = ArrayList<Map<String, Any>>()
                var limited = false
                var characters = 0
                val iterator = tess.resultIterator
                if (iterator != null) {
                    try {
                        iterator.begin()
                        do {
                            check(job)
                            val text = iterator.getUTF8Text(TessBaseAPI.PageIteratorLevel.RIL_TEXTLINE)?.trim() ?: ""
                            val rect = iterator.getBoundingBox(TessBaseAPI.PageIteratorLevel.RIL_TEXTLINE)
                            if (text.isNotEmpty()) {
                                if (lines.size >= 64 || text.length > 1024 || characters + text.length > 8192) { limited = true; break }
                                if (rect.size != 4 || rect[0] < 0 || rect[1] < 0 || rect[2] > targetW || rect[3] > targetH ||
                                    rect[2] <= rect[0] || rect[3] <= rect[1]) throw IllegalArgumentException()
                                characters += text.length
                                lines.add(mapOf("text" to text, "rect" to rect.toList()))
                            }
                        } while (iterator.next(TessBaseAPI.PageIteratorLevel.RIL_TEXTLINE))
                    } finally { iterator.delete() }
                }
                check(job)
                reply = mapOf("requestId" to id, "sha256" to hash, "width" to targetW,
                    "height" to targetH, "limited" to limited, "lines" to lines)
            } catch (_: InterruptedException) {
                failure = "CANCELLED"
            } catch (_: IllegalArgumentException) {
                failure = "LIMIT"
            } catch (_: Throwable) {
                failure = "UNAVAILABLE"
            } finally {
                try { api?.recycle() } catch (_: Throwable) { failure = "UNAVAILABLE"; reply = null }
                bitmap?.recycle()
                // Release only after native work/iterator/image have actually finished.
                active.compareAndSet(job, null)
            }
            main.post {
                if (closed.get()) { result.error("CANCELLED", "Local view closed", null) }
                else if (failure != null) { result.error(failure!!, "Local check incomplete", null) }
                else if (job.cancelled.get() || SystemClock.elapsedRealtime() >= job.deadline) { result.error("CANCELLED", "Local check stopped", null) }
                else result.success(reply)
            }
        }
    }
    private fun check(job: Job) {
        if (job.cancelled.get() || closed.get() || SystemClock.elapsedRealtime() >= job.deadline) throw InterruptedException()
    }
    private fun installModels(job: Job): String {
        // Model files only: no images, OCR text or approvals are written to disk.
        val root = File(context.noBackupFilesDir, "birdtie-ocr-$modelCommit")
        val dir = File(root, "tessdata")
        if (!dir.isDirectory && !dir.mkdirs()) throw IllegalStateException()
        for ((language, expected) in models) {
            check(job)
            val target = File(dir, "$language.traineddata")
            if (target.isFile && target.length() <= 5 * 1024 * 1024 && digest(target.readBytes()) == expected) continue
            val temporary = File(dir, "$language.tmp")
            try {
                context.assets.open("birdtie_ocr/tessdata/$language.traineddata").use { input ->
                    temporary.outputStream().use { output ->
                        val buffer = ByteArray(16384); var total = 0
                        while (true) {
                            check(job)
                            val count = input.read(buffer); if (count < 0) break
                            total += count; if (total > 5 * 1024 * 1024) throw IllegalArgumentException()
                            output.write(buffer, 0, count)
                        }
                    }
                }
                check(job)
                if (digest(temporary.readBytes()) != expected) throw IllegalStateException()
                if (target.exists() && !target.delete()) throw IllegalStateException()
                if (!temporary.renameTo(target)) throw IllegalStateException()
            } finally { if (temporary.exists()) temporary.delete() }
        }
        return root.absolutePath + File.separator
    }
    fun close() {
        closed.set(true)
        active.get()?.let { if (it.owner === this) it.cancelled.set(true) }
        channel.setMethodCallHandler(null)
    }
}
