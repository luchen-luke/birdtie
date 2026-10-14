package app.civu.civu_mobile

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.pm.PackageManager
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioRecord
import android.media.MediaRecorder
import android.os.Build
import android.os.Handler
import android.os.SystemClock
import android.util.Log
import io.flutter.plugin.common.MethodChannel
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/** Bounded Agent-only PCM capture and offline decoding; no file/audio export. */
class BundledOfflineSpeech(
    private val activity: Activity,
    private val handler: Handler,
    private val onPhase: (String, String) -> Unit,
    private val onFailure: (MethodChannel.Result, String, String, String, String) -> Unit,
    private val simplifiedChineseOutputRequired: Boolean = true,
) {
    companion object {
        private const val MODEL = "ggml-base-q5_1.bin"
        private const val MODEL_SIZE = 59707625L
        private const val MODEL_SHA = "422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898"
        private const val CODE_LICENSE_SHA = "94f29bbed6a22c35b992c5c6ebf0e7c92f13b836b90f36f461c9cf2f0f1d010d"
        private const val MODEL_LICENSE_SHA = "b5d65a59060e68c4ff940e1eddfa6f94b2d68fdf58ed7f4dd57721c997e35e9d"
        private const val MANIFEST_SHA = "9da8b25db1b2a5f897c5c300ee7a78b30eba04e2e9c80e5a28920ab6ba7a4763"
        private const val MAX_SAMPLES = 16000 * 30
        private val libraryReady = try { System.loadLibrary("birdtie_asr"); true } catch (_: LinkageError) { false } catch (_: SecurityException) { false }
    }
    // A selected build capability, not a claim that its asset/ABI is ready.
    val capabilityDeclared = true
    private val executor = Executors.newSingleThreadExecutor { task -> Thread(task, "BirdtieOfflineVoice") }
    private val audioManager = activity.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    @Volatile private var closed = false
    @Volatile private var resumed = false
    @Volatile private var inFlight = false
    @Volatile private var checking = false
    @Volatile private var qaTimingEnabled = false
    @Volatile private var current: Job? = null
    private var check: Check? = null
    val busy: Boolean get() = inFlight || checking || current != null || check != null
    private class Check(val id: String, val result: MethodChannel.Result) {
        var timeout: Runnable? = null
    }
    private class Job(val id: String, val result: MethodChannel.Result) {
        val beganAt = SystemClock.elapsedRealtime()
        val cancelled = AtomicBoolean(false)
        val stopping = AtomicBoolean(false)
        val capturing = AtomicBoolean(false)
        val answered = AtomicBoolean(false)
        val recordingLock = Any()
        @Volatile var handle = 0L
        @Volatile var recorder: AudioRecord? = null
        @Volatile var phase = QaPhase.PREPARE
        var deadline: Runnable? = null
        var totalDeadline: Runnable? = null
        var focus: AudioFocusRequest? = null
        var legacyFocus: AudioManager.OnAudioFocusChangeListener? = null
    }
    private enum class QaPhase(val wire: String) { PREPARE("prepare"), CAPTURE("capture"), NATIVE_CALL("native_call") }
    private enum class QaStage(val wire: String) {
        PREPARE_ENTER("prepare_enter"), PREPARE_EXIT("prepare_exit"),
        CAPTURE_ENTER("capture_enter"), CAPTURE_EXIT("capture_exit"),
        STOP_REQUEST("stop_request"), NATIVE_CALL_ENTER("native_call_enter"),
        NATIVE_CALL_EXIT("native_call_exit"), PHASE_TIMEOUT("phase_timeout"),
        TOTAL_TIMEOUT("total_timeout"), RETIRED("retired"), WORKER_EXIT("worker_exit")
    }
    private enum class CaptureExit(val wire: String) {
        NONE("none"), MANUAL_STOP("manual_stop"), LIMIT("limit"), CANCELLED("cancelled"), AUDIO_ERROR("audio_error")
    }
    // Uses only the host's already existing explicit preview Intent switch.
    // Toggling diagnostics does not create, stop, retry or publish a voice job.
    fun setQaTiming(enabled: Boolean) {
        qaTimingEnabled = !closed && enabled && activity.packageName == "app.civu.civu_mobile.birdtiepreview"
    }
    private fun qa(job: Job, stage: QaStage, count: Int? = null, exit: CaptureExit = CaptureExit.NONE) {
        if (!qaTimingEnabled) return
        try {
            val elapsed = (SystemClock.elapsedRealtime() - job.beganAt).coerceIn(0L, 120000L)
            val boundedCount = count?.coerceIn(0, MAX_SAMPLES)
            val duration = boundedCount?.let { it * 1000L / 16000L }
            Log.d("BirdtieBundledAsr", "stage=${stage.wire} phase=${job.phase.wire} elapsedMs=$elapsed sampleCount=${boundedCount ?: "none"} audioDurationMs=${duration ?: "none"} captureExit=${exit.wire}")
        } catch (_: Exception) {
            // Observation failure must not change the voice job or cleanup.
        } catch (_: OutOfMemoryError) {
            // In particular, diagnostics must not bypass PCM/context release.
        }
    }
    private external fun nativeCreate(): Long
    private external fun nativePrepare(handle: Long, path: String): Boolean
    private external fun nativeTranscribe(handle: Long, pcm: ShortArray, count: Int): String?
    private external fun nativeDetectedLanguage(handle: Long): String?
    private external fun nativeCancel(handle: Long)
    private external fun nativeRelease(handle: Long)

    // Output form is an explicit application requirement, never the ASR language.
    private fun preferredDraftForm(original: String, detectedLanguage: String?): String {
        if (!simplifiedChineseOutputRequired || detectedLanguage != "zh" ||
            Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return original
        return try { ChineseFormApi29.simplify(original) }
        catch (_: Exception) { original }
        catch (_: LinkageError) { original }
        catch (_: OutOfMemoryError) { original }
    }
    @android.annotation.TargetApi(29)
    private object ChineseFormApi29 {
        fun simplify(original: String): String {
            val converter = android.icu.text.Transliterator.getInstance("Hant-Hans")
            converter.setFilter(android.icu.text.UnicodeSet("[:Han:]"))
            return converter.transliterate(original)
        }
    }

    private fun hash(bytes: ByteArray): String = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it.toInt() and 255) }
    private fun hash(file: File): String {
        val digest = MessageDigest.getInstance("SHA-256")
        val buffer = ByteArray(32768)
        file.inputStream().use { input -> while (true) { val count = input.read(buffer); if (count < 0) break; digest.update(buffer, 0, count) } }
        buffer.fill(0)
        return digest.digest().joinToString("") { "%02x".format(it.toInt() and 255) }
    }
    private fun smallAsset(name: String): ByteArray? {
        val bytes = ByteArray(16385)
        var count = 0
        activity.assets.open("birdtie_asr/$name").use { input ->
            while (count < bytes.size) {
                val read = input.read(bytes, count, bytes.size - count)
                if (read < 0) break
                if (read == 0) throw IllegalStateException("Asset unavailable")
                count += read
            }
        }
        return if (count > 16384) null else bytes.copyOf(count)
    }
    private fun verifiedModel(): File? {
        if (!libraryReady || closed) return null
        val manifestBytes = smallAsset("model-manifest.json") ?: return null
        if (hash(manifestBytes) != MANIFEST_SHA) return null
        val manifest = JSONObject(String(manifestBytes, Charsets.UTF_8))
        if (manifest.optString("modelId") != "whisper-base-multilingual-q5_1" ||
            manifest.optString("fileName") != MODEL || manifest.optLong("byteLength") != MODEL_SIZE ||
            manifest.optString("sha256") != MODEL_SHA || manifest.optString("license") != "MIT") return null
        for ((name, expected) in listOf("LICENSE.whisper.cpp.MIT" to CODE_LICENSE_SHA, "LICENSE.OpenAI.MIT" to MODEL_LICENSE_SHA)) {
            val bytes = smallAsset(name) ?: return null
            if (hash(bytes) != expected) return null
        }
        val directory = File(activity.noBackupFilesDir, "birdtie_asr")
        if (!directory.isDirectory && !directory.mkdirs()) return null
        val target = File(directory, MODEL)
        if (target.isFile && target.length() == MODEL_SIZE && hash(target) == MODEL_SHA) return target
        run {
            // This copy contains public model weights only, never microphone data.
            val temporary = File.createTempFile("model-", ".part", directory)
            try {
                activity.assets.open("birdtie_asr/$MODEL").use { input -> temporary.outputStream().use { output ->
                    val buffer = ByteArray(32768); var total = 0L
                    while (true) { val count = input.read(buffer); if (count < 0) break; total += count; if (total > MODEL_SIZE) throw IllegalStateException("Model bound"); output.write(buffer, 0, count) }
                    buffer.fill(0)
                } }
                if (temporary.length() != MODEL_SIZE || hash(temporary) != MODEL_SHA) return null
                if (target.exists() && !target.delete()) return null
                if (!temporary.renameTo(target)) return null
            } finally { temporary.delete() }
        }
        // The same verified public-weight file was atomically renamed above;
        // avoid hashing it a second time within this readiness operation.
        return target
    }
    fun checkReady(id: String, result: MethodChannel.Result) {
        if (busy || closed) { onFailure(result, id, "BUSY", "support", "busy"); return }
        val pending = Check(id, result); check = pending; checking = true
        pending.timeout = Runnable { finishCheck(pending, false) }
        handler.postDelayed(pending.timeout!!, 10000L)
        try {
            executor.execute {
                val ready = try { verifiedModel() != null } catch (_: Exception) { false } catch (_: LinkageError) { false } catch (_: OutOfMemoryError) { false }
                checking = false
                handler.post { finishCheck(pending, !closed && resumed && ready) }
            }
        } catch (_: Exception) { checking = false; finishCheck(pending, false) }
        catch (_: OutOfMemoryError) { checking = false; finishCheck(pending, false) }
    }
    private fun finishCheck(pending: Check, ready: Boolean) {
        if (check !== pending) return
        check = null
        pending.timeout?.let(handler::removeCallbacks)
        pending.result.success(mapOf("requestId" to pending.id, "ready" to ready))
    }
    fun start(id: String, result: MethodChannel.Result) {
        if (busy || closed || !resumed) { onFailure(result, id, "BUSY", "start", "busy"); return }
        val job = Job(id, result); current = job; inFlight = true
        job.totalDeadline = Runnable { if (current === job) { qa(job, QaStage.TOTAL_TIMEOUT); retire(job, "TIMEOUT", "timeout", "timeout") } }
        handler.postDelayed(job.totalDeadline!!, 100000L)
        deadline(job, 10000L)
        try { executor.execute { run(job) } }
        catch (_: Exception) { inFlight = false; retire(job, "UNAVAILABLE", "create", "service") }
        catch (_: OutOfMemoryError) { inFlight = false; retire(job, "UNAVAILABLE", "create", "service") }
    }
    private fun deadline(job: Job, millis: Long) {
        job.deadline?.let(handler::removeCallbacks)
        job.deadline = Runnable { if (current === job) { qa(job, QaStage.PHASE_TIMEOUT); retire(job, "TIMEOUT", "timeout", "timeout") } }
        handler.postDelayed(job.deadline!!, millis)
    }
    @Suppress("DEPRECATION")
    private fun focus(job: Job): Boolean {
        val listener = AudioManager.OnAudioFocusChangeListener { change -> if (change < 0) handler.post { if (current === job && job.capturing.get()) retire(job, "CANCELLED", "lifecycle", "cancelled") } }
        return if (Build.VERSION.SDK_INT >= 26) {
            val request = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT)
                .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
                .setOnAudioFocusChangeListener(listener, handler).build()
            job.focus = request
            audioManager.requestAudioFocus(request) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED
        } else { job.legacyFocus = listener; audioManager.requestAudioFocus(listener, AudioManager.STREAM_MUSIC, AudioManager.AUDIOFOCUS_GAIN_TRANSIENT) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED }
    }
    @Suppress("DEPRECATION")
    private fun abandonFocus(job: Job) {
        if (Build.VERSION.SDK_INT >= 26) job.focus?.let { audioManager.abandonAudioFocusRequest(it) }
        job.legacyFocus?.let { audioManager.abandonAudioFocus(it) }
        job.focus = null; job.legacyFocus = null
    }
    private fun run(job: Job) {
        var pcm: ShortArray? = null
        var buffer: ShortArray? = null
        var stage = "create"
        var failure = "unsupported"
        var count = 0
        var captureEntered = false
        var captureExited = false
        try {
            if (job.cancelled.get()) return
            qa(job, QaStage.PREPARE_ENTER)
            val model = verifiedModel() ?: throw IllegalStateException("Model unavailable")
            job.handle = nativeCreate()
            if (job.cancelled.get()) { nativeCancel(job.handle); return }
            if (job.handle == 0L || !nativePrepare(job.handle, model.absolutePath)) throw IllegalStateException("Model unavailable")
            qa(job, QaStage.PREPARE_EXIT)
            if (job.cancelled.get()) return
            if (job.stopping.get()) {
                handler.post { if (current === job) retire(job, "NO_SPEECH", "stop", "no_speech") }; return
            }
            if (closed || !resumed) {
                handler.post { if (current === job) retire(job, "CANCELLED", "lifecycle", "cancelled") }; return
            }
            if (activity.checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
                handler.post { if (current === job) retire(job, "PERMISSION", "permission", "permission") }; return
            }
            stage = "start"; failure = "audio"
            job.capturing.set(true)
            if (!focus(job)) throw IllegalStateException("Audio unavailable")
            val minimum = AudioRecord.getMinBufferSize(16000, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
            if (minimum <= 0) throw IllegalStateException("Audio unavailable")
            val recorder = AudioRecord(MediaRecorder.AudioSource.MIC, 16000, AudioFormat.CHANNEL_IN_MONO,
                AudioFormat.ENCODING_PCM_16BIT, maxOf(minimum, 6400))
            job.recorder = recorder
            if (job.cancelled.get()) return
            if (recorder.state != AudioRecord.STATE_INITIALIZED || recorder.sampleRate != 16000 ||
                recorder.channelCount != 1 || recorder.audioFormat != AudioFormat.ENCODING_PCM_16BIT) {
                throw IllegalStateException("Audio unavailable")
            }
            val captured = ShortArray(MAX_SAMPLES); pcm = captured
            val block = ShortArray(1600); buffer = block
            val started = synchronized(job.recordingLock) {
                if (job.cancelled.get() || job.stopping.get() || closed || !resumed) false
                else {
                    if (activity.checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) throw SecurityException("Microphone permission required")
                    recorder.startRecording()
                    if (recorder.recordingState != AudioRecord.RECORDSTATE_RECORDING) throw IllegalStateException("Audio unavailable")
                    true
                }
            }
            if (!started) {
                handler.post { if (current === job) retire(job, if (job.stopping.get()) "NO_SPEECH" else "CANCELLED", "stop", if (job.stopping.get()) "no_speech" else "cancelled") }; return
            }
            val began = SystemClock.elapsedRealtime()
            job.phase = QaPhase.CAPTURE
            captureEntered = true
            qa(job, QaStage.CAPTURE_ENTER)
            stage = "recognition"
            handler.post { if (current === job && !job.cancelled.get() && !job.stopping.get() && resumed) { deadline(job, 31000L); onPhase(job.id, "listening") } }
            while (!job.cancelled.get() && !job.stopping.get() && count < MAX_SAMPLES && SystemClock.elapsedRealtime() - began < 30000L) {
                if (activity.checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) throw SecurityException("Microphone permission required")
                // Policy interruption is an audio failure, not silent speech
                // sent to the decoder. No call or other-app identity is read.
                if (Build.VERSION.SDK_INT >= 29 && recorder.activeRecordingConfiguration?.isClientSilenced == true) throw IllegalStateException("Audio unavailable")
                val received = recorder.read(block, 0, minOf(block.size, MAX_SAMPLES - count), AudioRecord.READ_BLOCKING)
                if (received < 0) { if (job.cancelled.get() || job.stopping.get()) break; throw IllegalStateException("Audio unavailable") }
                if (received > 0) { block.copyInto(captured, count, 0, received); count += received; block.fill(0) }
            }
            synchronized(job.recordingLock) {
                try { recorder.stop() } catch (_: Exception) {}
                recorder.release(); job.recorder = null
            }
            job.capturing.set(false)
            abandonFocus(job) // Finished capture must not own media focus during CPU decode.
            captureExited = true
            qa(job, QaStage.CAPTURE_EXIT, count, when {
                job.cancelled.get() -> CaptureExit.CANCELLED
                job.stopping.get() -> CaptureExit.MANUAL_STOP
                else -> CaptureExit.LIMIT
            })
            if (job.cancelled.get()) return
            job.stopping.set(true)
            handler.post { if (current === job && resumed) { deadline(job, 60000L); onPhase(job.id, "processing") } }
            if (count < 1600) {
                handler.post { if (current === job) retire(job, "NO_SPEECH", "recognition", "no_speech") }; return
            }
            failure = "service"
            job.phase = QaPhase.NATIVE_CALL
            // This is the actual JNI call boundary. Its internal energy gate
            // may return before whisper_full; do not call this encoder proof.
            qa(job, QaStage.NATIVE_CALL_ENTER, count)
            val decoded = nativeTranscribe(job.handle, captured, count)
            qa(job, QaStage.NATIVE_CALL_EXIT, count)
            val originalText = decoded?.trim() ?: throw IllegalStateException("Decode unavailable")
            val detectedLanguage = if (originalText.isBlank() || job.cancelled.get()) null else try {
                nativeDetectedLanguage(job.handle)
            } catch (_: Exception) { null } catch (_: LinkageError) { null } catch (_: OutOfMemoryError) { null }
            val text = preferredDraftForm(originalText, detectedLanguage)
            handler.post {
                if (current !== job || job.cancelled.get() || closed || !resumed) return@post
                if (text.isNullOrBlank()) retire(job, "NO_SPEECH", "recognition", "no_speech")
                else if (text.length > 4096) retire(job, "INVALID", "recognition", "invalid_request")
                else { current = null; job.deadline?.let(handler::removeCallbacks); job.totalDeadline?.let(handler::removeCallbacks); if (job.answered.compareAndSet(false, true)) job.result.success(mapOf("requestId" to job.id, "text" to text)) }
            }
        } catch (_: SecurityException) {
            handler.post { if (current === job && !job.cancelled.get()) retire(job, "PERMISSION", "permission", "permission") }
        } catch (_: Exception) {
            val failedStage = stage; val failedReason = failure
            handler.post { if (current === job && !job.cancelled.get()) retire(job, "UNAVAILABLE", failedStage, failedReason) }
        } catch (_: LinkageError) {
            handler.post { if (current === job && !job.cancelled.get()) retire(job, "UNAVAILABLE", "create", "unsupported") }
        } catch (_: OutOfMemoryError) {
            handler.post { if (current === job && !job.cancelled.get()) retire(job, "UNAVAILABLE", "create", "service") }
        } finally {
            if (captureEntered && !captureExited) qa(job, QaStage.CAPTURE_EXIT, count,
                if (job.cancelled.get()) CaptureExit.CANCELLED else CaptureExit.AUDIO_ERROR)
            job.capturing.set(false)
            pcm?.fill(0); buffer?.fill(0)
            synchronized(job.recordingLock) {
                job.recorder?.let { try { it.stop() } catch (_: Exception) {}; try { it.release() } catch (_: Exception) {} }; job.recorder = null
            }
            if (job.handle != 0L) { try { nativeRelease(job.handle) } catch (_: LinkageError) {} catch (_: Exception) {}; job.handle = 0L }
            try { abandonFocus(job) } catch (_: Exception) {}
            inFlight = false
            qa(job, QaStage.WORKER_EXIT)
            handler.post { if (current === job && job.cancelled.get()) current = null }
        }
    }
    fun stop(id: String) { current?.takeIf { it.id == id && !it.cancelled.get() }?.let { qa(it, QaStage.STOP_REQUEST); it.stopping.set(true); synchronized(it.recordingLock) { try { it.recorder?.stop() } catch (_: Exception) {} } } }
    private fun retire(job: Job, code: String, stage: String, reason: String) {
        qa(job, QaStage.RETIRED)
        if (current === job) current = null
        job.cancelled.set(true); job.capturing.set(false); job.deadline?.let(handler::removeCallbacks); job.totalDeadline?.let(handler::removeCallbacks)
        if (job.handle != 0L) try { nativeCancel(job.handle) } catch (_: LinkageError) {}
        synchronized(job.recordingLock) { try { job.recorder?.stop() } catch (_: Exception) {} }
        if (job.answered.compareAndSet(false, true)) onFailure(job.result, job.id, code, stage, reason)
    }
    fun cancel(id: String? = null) {
        current?.takeIf { id == null || it.id == id }?.let { retire(it, "CANCELLED", "lifecycle", "cancelled") }
        check?.takeIf { id == null || it.id == id }?.let { finishCheck(it, false) }
    }
    fun onResume() { resumed = true }
    fun onPause() { resumed = false; cancel() }
    fun close() { if (closed) return; closed = true; resumed = false; cancel(); executor.shutdown() }
}
