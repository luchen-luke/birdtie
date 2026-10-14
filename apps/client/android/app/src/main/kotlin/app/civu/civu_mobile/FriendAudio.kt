package app.civu.civu_mobile

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.pm.PackageManager
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.media.MediaPlayer
import android.media.MediaRecorder
import android.os.Handler
import android.os.Looper
import android.util.Base64
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.security.MessageDigest

/** Human message files only. No SpeechRecognizer, Agent or network integration. */
class FriendAudio(private val activity: Activity, messenger: BinaryMessenger) : MethodChannel.MethodCallHandler {
    private val channel = MethodChannel(messenger, "birdtie/friend_audio")
    private val handler = Handler(Looper.getMainLooper())
    private val audioManager = activity.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    private var focus: AudioFocusRequest? = null
    private var closed = false
    private var resumed = true
    private var record: Recording? = null
    private var permission: Permission? = null
    private var player: MediaPlayer? = null
    private var playFile: File? = null
    private var playID: String? = null
    private class Recording(val id: String, val file: File, var recorder: MediaRecorder?)
    private class Permission(val id: String, val code: Int, val result: MethodChannel.Result, var granted: Boolean = false)
    companion object { private var permissionCode = 10000; private const val MAX_BYTES = 1048576; private const val MAX_MILLIS = 90000L }
    init { channel.setMethodCallHandler(this) }
    private fun event(id: String, phase: String) { if (!closed) channel.invokeMethod("state", mapOf("requestId" to id, "phase" to phase)) }
    private fun id(call: MethodCall): String? = call.argument<String>("requestId")?.takeIf { it.length in 1..128 && it.all { c -> c.isLetterOrDigit() || c == '-' } }
    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        if (closed) { result.error("UNAVAILABLE", "Audio owner closed", null); return }
        val id = id(call)
        if (id == null) { result.error("INVALID", "Invalid audio request", null); return }
        try { when (call.method) {
            "start" -> start(id, result)
            "stop" -> stop(id, result)
            "cancel" -> { cancel(id); result.success(null) }
            "play" -> play(id, call, result)
            "stopPlayback" -> { if (playID == id) stopPlayback(); result.success(null) }
            else -> result.notImplemented()
        } } catch (_: Exception) { result.error("UNAVAILABLE", "Audio operation failed", null) }
    }
    private fun granted() = activity.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
    private fun start(id: String, result: MethodChannel.Result) {
        if (!resumed || record != null || permission != null) { result.error("BUSY", "Audio capture unavailable", null); return }
        stopPlayback()
        if (granted()) { startGranted(id, result); return }
        if (permissionCode >= 19999) { result.error("UNAVAILABLE", "Permission request limit", null); return }
        val pending = Permission(id, ++permissionCode, result)
        permission = pending
        event(id, "permission")
        handler.postDelayed({ if (permission === pending) { permission = null; pending.result.error("TIMEOUT", "Permission timed out", null) } }, 45000L)
        try { activity.requestPermissions(arrayOf(Manifest.permission.RECORD_AUDIO), pending.code) }
        catch (_: Exception) { permission = null; result.error("PERMISSION", "Permission unavailable", null) }
    }
    fun onPermissionsResult(code: Int, permissions: Array<out String>, results: IntArray) {
        val p = permission?.takeIf { it.code == code } ?: return
        p.granted = permissions.size == 1 && permissions[0] == Manifest.permission.RECORD_AUDIO && results.size == 1 && results[0] == PackageManager.PERMISSION_GRANTED && granted()
        if (!p.granted) { permission = null; p.result.error("PERMISSION", "Microphone permission denied", null); return }
        if (resumed) finishPermission(p)
    }
    private fun finishPermission(p: Permission) { if (permission !== p || !p.granted || !resumed || closed) return; permission = null; startGranted(p.id, p.result) }
    private fun requestFocus(): Boolean {
        abandonFocus()
        val f = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT)
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
            .setOnAudioFocusChangeListener({ change -> if (change < 0) { interruptRecording(); stopPlayback() } }, handler).build()
        focus = f
        return audioManager.requestAudioFocus(f) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED
    }
    private fun abandonFocus() { focus?.let { audioManager.abandonAudioFocusRequest(it) }; focus = null }
    @Suppress("DEPRECATION")
    private fun startGranted(id: String, result: MethodChannel.Result) {
        if (!resumed || closed || !granted() || record != null || !requestFocus()) { result.error("UNAVAILABLE", "Microphone unavailable", null); return }
        val file = File.createTempFile("birdtie-human-voice-", ".m4a", activity.cacheDir)
        var recorder: MediaRecorder? = null
        try {
            recorder = MediaRecorder()
            val job = Recording(id, file, recorder)
            recorder.setAudioSource(MediaRecorder.AudioSource.MIC)
            recorder.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
            recorder.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
            recorder.setAudioChannels(1)
            recorder.setAudioSamplingRate(44100)
            recorder.setAudioEncodingBitRate(48000)
            recorder.setMaxDuration(89000)
            recorder.setMaxFileSize(MAX_BYTES.toLong() - 4096L)
            recorder.setOutputFile(file.absolutePath)
            recorder.setOnErrorListener { _, _, _ -> handler.post { if (record === job) interruptRecording() } }
            recorder.setOnInfoListener { _, what, _ -> if (what == MediaRecorder.MEDIA_RECORDER_INFO_MAX_DURATION_REACHED || what == MediaRecorder.MEDIA_RECORDER_INFO_MAX_FILESIZE_REACHED) handler.post { if (record === job) { try { job.recorder?.stop() } catch (_: Exception) {}; job.recorder?.release(); job.recorder = null } } }
            recorder.prepare(); recorder.start(); record = job
            event(id, "recording"); result.success(null)
            handler.postDelayed({ if (record === job) interruptRecording() }, 95000L)
        } catch (_: Exception) { try { recorder?.release() } catch (_: Exception) {}; file.delete(); abandonFocus(); result.error("RECORDING", "Could not start recording", null) }
    }
    private fun stop(id: String, result: MethodChannel.Result) {
        val job = record?.takeIf { it.id == id }
        if (job == null || !resumed) { result.error("CANCELED", "Recording is no longer current", null); return }
        record = null
        try {
            job.recorder?.stop(); job.recorder?.release(); job.recorder = null
            if (job.file.length() !in 1..MAX_BYTES.toLong()) throw IllegalStateException("Size")
            val bytes = job.file.readBytes()
            val duration = durationMillis(bytes)
            if (duration !in 1..MAX_MILLIS) throw IllegalStateException("Duration")
            result.success(mapOf("requestId" to id, "mimeType" to "audio/mp4", "byteSize" to bytes.size, "sha256" to hash(bytes), "durationMillis" to duration, "contentBase64" to Base64.encodeToString(bytes, Base64.NO_WRAP)))
        } catch (_: Exception) { result.error("RECORDING", "No usable audio recording", null) }
        finally { try { job.recorder?.release() } catch (_: Exception) {}; job.file.delete(); abandonFocus() }
    }
    private fun cancel(id: String) {
        permission?.takeIf { it.id == id }?.let { permission = null; it.result.error("CANCELED", "Recording canceled", null) }
        record?.takeIf { it.id == id }?.let { record = null; try { it.recorder?.reset(); it.recorder?.release() } catch (_: Exception) {}; it.file.delete(); abandonFocus() }
    }
    private fun interruptRecording() { val p = permission; if (p != null) { permission = null; p.result.error("CANCELED", "Recording canceled", null); event(p.id, "interrupted") }; val r = record; if (r != null) { cancel(r.id); event(r.id, "interrupted") } }
    private fun play(id: String, call: MethodCall, result: MethodChannel.Result) {
        if (!resumed || record != null || permission != null) { result.error("BUSY", "Audio unavailable", null); return }
        val encoded = call.argument<String>("contentBase64")
        val expected = call.argument<String>("sha256")
        if (encoded == null || encoded.length > 1398104 || expected == null || !Regex("[0-9a-f]{64}").matches(expected)) { result.error("INVALID", "Invalid audio content", null); return }
        stopPlayback()
        var file: File? = null
        var mediaPlayer: MediaPlayer? = null
        try {
            val bytes = Base64.decode(encoded, Base64.NO_WRAP)
            if (bytes.size !in 1..MAX_BYTES || hash(bytes) != expected || durationMillis(bytes) !in 1..MAX_MILLIS) throw IllegalStateException("Content")
            if (!requestFocus()) throw IllegalStateException("Focus")
            file = File.createTempFile("birdtie-human-play-", ".m4a", activity.cacheDir); file.writeBytes(bytes)
            mediaPlayer = MediaPlayer()
            mediaPlayer.setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
            mediaPlayer.setDataSource(file.absolutePath)
            mediaPlayer.setOnCompletionListener { if (playID == id) { stopPlayback(); event(id, "finished") } }
            mediaPlayer.setOnErrorListener { _, _, _ -> if (playID == id) { stopPlayback(); event(id, "finished") }; true }
            mediaPlayer.prepare(); player = mediaPlayer; playFile = file; playID = id; mediaPlayer.start(); result.success(null)
        } catch (_: Exception) { try { mediaPlayer?.release() } catch (_: Exception) {}; file?.delete(); abandonFocus(); result.error("PLAYBACK", "Audio playback failed", null) }
    }
    private fun stopPlayback() { val id = playID; playID = null; try { player?.stop(); player?.release() } catch (_: Exception) {}; player = null; playFile?.delete(); playFile = null; abandonFocus(); if (id != null) event(id, "finished") }
    private fun hash(b: ByteArray) = MessageDigest.getInstance("SHA-256").digest(b).joinToString("") { "%02x".format(it.toInt() and 255) }
    private data class Atom(val name: String, val start: Int, val end: Int)
    private fun u32(b: ByteArray, p: Int): Long = ByteBuffer.wrap(b,p,4).order(ByteOrder.BIG_ENDIAN).int.toLong() and 0xffffffffL
    private fun atoms(b: ByteArray, start: Int, end: Int): List<Atom> {
        val out = mutableListOf<Atom>(); var p = start
        while (p < end) { if (end-p<8 || out.size>4096) throw IllegalArgumentException("Atom"); var n=u32(b,p); var header=8; if(n==1L){if(end-p<16)throw IllegalArgumentException("Atom");n=ByteBuffer.wrap(b,p+8,8).order(ByteOrder.BIG_ENDIAN).long;header=16};if(n<header||n>end-p)throw IllegalArgumentException("Atom");out.add(Atom(String(b,p+4,4,Charsets.US_ASCII),p+header,p+n.toInt()));p+=n.toInt() }
        return out
    }
    private fun child(b: ByteArray, a: Atom, name: String) = atoms(b,a.start,a.end).filter { it.name==name }.single()
    // Same actual mdhd track clock and rounding as the Go contract; no trust in
    // wall-clock elapsed time or caller-provided duration.
    private fun durationMillis(b: ByteArray): Long {
        val moov=atoms(b,0,b.size).filter { it.name=="moov" }.single();val trak=child(b,moov,"trak");val mdia=child(b,trak,"mdia");val h=child(b,mdia,"hdlr")
        if(h.end-h.start<12||String(b,h.start+8,4,Charsets.US_ASCII)!="soun")throw IllegalArgumentException("Track")
        val a=child(b,mdia,"mdhd");if(a.end-a.start<20)throw IllegalArgumentException("Clock")
        val version=b[a.start].toInt();val scale:Long;val ticks:Long
        if(version==0){scale=u32(b,a.start+12);ticks=u32(b,a.start+16)}else if(version==1&&a.end-a.start>=32){scale=u32(b,a.start+20);ticks=ByteBuffer.wrap(b,a.start+24,8).order(ByteOrder.BIG_ENDIAN).long}else throw IllegalArgumentException("Clock")
        if(scale<=0||ticks<=0||ticks>scale*90)throw IllegalArgumentException("Clock")
        return (ticks*1000+scale/2)/scale
    }
    fun onResume() { resumed=true; permission?.let { if(it.granted)handler.post{finishPermission(it)} } }
    fun onPause() { resumed=false; if(permission==null)interruptRecording(); stopPlayback() }
    fun onStop() { if(permission!=null)interruptRecording(); if(record!=null)interruptRecording(); stopPlayback() }
    fun close() { if(closed)return; interruptRecording(); stopPlayback(); closed=true; handler.removeCallbacksAndMessages(null); channel.setMethodCallHandler(null) }
}
