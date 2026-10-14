package app.civu.civu_mobile

import android.Manifest
import android.app.Activity
import android.app.ActivityOptions
import android.content.ComponentName
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ApplicationInfo
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.speech.RecognitionListener
import android.speech.RecognitionService
import android.speech.RecognitionSupport
import android.speech.RecognitionSupportCallback
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import android.util.Log
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel

/** Android owns audio capture. BirdTie discards callback buffers without storing
 * or uploading them, and returns only final text through the Flutter channel.
 * The system route is explicitly bound to the service disclosed for this job;
 * PREFER_OFFLINE is deliberately not used as a privacy guarantee.
 */
class VoiceInputRecognizer(private val activity: Activity, messenger: BinaryMessenger) : MethodChannel.MethodCallHandler {
    private val channel = MethodChannel(messenger, "birdtie/voice_input")
    private val handler = Handler(Looper.getMainLooper())
    private var closed = false
    private var resumed = false
    private var permission: PermissionJob? = null
    private var support: SupportJob? = null
    private var active: Job? = null
    private var external: ExternalJob? = null
    private var qaTimingEnabled = false
    companion object {
        // Never recycle a permission request code in this process. A cancelled
        // Android dialog can still reply after another Dart gesture starts.
        private var permissionCode = 23999
    }
    private data class Service(val component: ComponentName, val name: String)
    private enum class Stage(val wire: String) {
        AVAILABILITY("availability"), PERMISSION("permission"), SUPPORT("support"),
        CREATE("create"), START("start"), RECOGNITION("recognition"), STOP("stop"),
        TIMEOUT("timeout"), LIFECYCLE("lifecycle")
    }
    private enum class Route(val wire: String) { ON_DEVICE("on_device"), SYSTEM("system"), SYSTEM_ACTIVITY("system_activity"), UNKNOWN("unknown") }
    private enum class Reason(val wire: String) {
        AVAILABLE("available"), NETWORK_TIMEOUT("network_timeout"), NETWORK("network"),
        AUDIO("audio"), SERVICE("service"), CLIENT("client"), NO_SPEECH("no_speech"),
        BUSY("busy"), PERMISSION("permission"), RATE_LIMIT("rate_limit"), LANGUAGE("language"),
        UNSUPPORTED("unsupported"), INVALID_STATE("invalid_state"), INVALID_REQUEST("invalid_request"),
        CANCELLED("cancelled"), TIMEOUT("timeout"), SERVICE_CHANGED("service_changed"), UNKNOWN("unknown")
    }
    private val bundled = BundledOfflineSpeech(activity, handler,
        onPhase = { request, state ->
            if (!closed && resumed) channel.invokeMethod("phase", mapOf("requestId" to request, "state" to state))
        },
        onFailure = { result, request, code, stage, failure ->
            fail(result, request, code,
                Stage.entries.firstOrNull { it.wire == stage } ?: Stage.CREATE,
                Route.ON_DEVICE,
                Reason.entries.firstOrNull { it.wire == failure } ?: Reason.UNKNOWN)
        })
    private enum class TimingStage(val wire: String) {
        LEASE_REQUEST("lease_request"), LEASE_ACK("lease_ack"), DISPLAY_BOUND("display_bound"),
        LAUNCH("launch"), LAUNCH_RETURN("launch_return"), HOST_PAUSE("host_pause"),
        HOST_STOP("host_stop"), RESULT("result"), HOST_RESUME("host_resume"),
        DELIVERED("delivered"), RETIRED("retired"), TIMEOUT("timeout")
    }
    private class PermissionJob(val id: String, val code: Int, val result: MethodChannel.Result) {
        var granted: Boolean? = null
        var timeout: Runnable? = null
    }
    private class SupportJob(val id: String, val recognizer: SpeechRecognizer, val result: MethodChannel.Result) {
        var timeout: Runnable? = null
    }
    private class Job(val id: String, val recognizer: SpeechRecognizer, val result: MethodChannel.Result, val route: Route) {
        var stopping = false
        var timeout: Runnable? = null
    }
    private class ExternalJob(val id: String, val code: Int, val result: MethodChannel.Result, val displayId: Int) {
        val beganAt = SystemClock.elapsedRealtime()
        var launched = false
        var received = false
        var text: String? = null
        var resultCode: Int? = null
        var failure: Reason? = null
        var timeout: Runnable? = null
    }
    init { channel.setMethodCallHandler(this) }

    // Local preview only, in memory and OFF unless its explicit launch Intent
    // opts in. The switch does not create, retry, retire or send a voice job.
    fun setQaTiming(enabled: Boolean) {
        qaTimingEnabled = !closed && enabled && activity.packageName == "app.civu.civu_mobile.birdtiepreview"
        bundled.setQaTiming(qaTimingEnabled)
    }
    private fun timing(stage: TimingStage, job: ExternalJob, failure: Reason? = null) {
        if (!qaTimingEnabled) return
        val elapsed = (SystemClock.elapsedRealtime() - job.beganAt).coerceIn(0L, 120000L)
        Log.d("BirdtieVoiceTiming", "stage=${stage.wire} elapsedMs=$elapsed reason=${failure?.wire ?: "none"}")
    }
    private fun callerDisplayId(): Int? = try {
        activity.window.decorView.display?.displayId?.takeIf { it >= 0 }
    } catch (_: Exception) { null }

    // Debuggable builds only. Every value is a closed enum/numeric reason; never
    // log service names, request IDs, exception messages, voice or transcripts.
    private fun diagnostic(stage: Stage, route: Route, reason: Reason, androidError: Int? = null) {
        if ((activity.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE) == 0) return
        val numeric = androidError?.takeIf { it in 1..15 }
        Log.d("BirdtieVoiceInput", "stage=${stage.wire} route=${route.wire} reason=${reason.wire} androidError=${numeric ?: "none"}")
    }
    private fun reason(code: String): Reason = when (code) {
        "PERMISSION" -> Reason.PERMISSION
        "NO_SPEECH" -> Reason.NO_SPEECH
        "BUSY" -> Reason.BUSY
        "LANGUAGE" -> Reason.LANGUAGE
        "CANCELLED" -> Reason.CANCELLED
        "TIMEOUT" -> Reason.TIMEOUT
        "SERVICE_CHANGED" -> Reason.SERVICE_CHANGED
        "INVALID" -> Reason.INVALID_REQUEST
        else -> Reason.UNKNOWN
    }
    private fun androidReason(error: Int): Reason = when (error) {
        SpeechRecognizer.ERROR_NETWORK_TIMEOUT -> Reason.NETWORK_TIMEOUT
        SpeechRecognizer.ERROR_NETWORK -> Reason.NETWORK
        SpeechRecognizer.ERROR_AUDIO -> Reason.AUDIO
        SpeechRecognizer.ERROR_SERVER, SpeechRecognizer.ERROR_SERVER_DISCONNECTED -> Reason.SERVICE
        SpeechRecognizer.ERROR_CLIENT -> Reason.CLIENT
        SpeechRecognizer.ERROR_NO_MATCH, SpeechRecognizer.ERROR_SPEECH_TIMEOUT -> Reason.NO_SPEECH
        SpeechRecognizer.ERROR_RECOGNIZER_BUSY -> Reason.BUSY
        SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS -> Reason.PERMISSION
        SpeechRecognizer.ERROR_TOO_MANY_REQUESTS -> Reason.RATE_LIMIT
        SpeechRecognizer.ERROR_LANGUAGE_NOT_SUPPORTED, SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE -> Reason.LANGUAGE
        SpeechRecognizer.ERROR_CANNOT_CHECK_SUPPORT, SpeechRecognizer.ERROR_CANNOT_LISTEN_TO_DOWNLOAD_EVENTS -> Reason.UNSUPPORTED
        else -> Reason.UNKNOWN
    }
    private fun exceptionReason(error: Exception): Reason = when (error) {
        is SecurityException -> Reason.PERMISSION
        is UnsupportedOperationException -> Reason.UNSUPPORTED
        is IllegalStateException -> Reason.INVALID_STATE
        is IllegalArgumentException -> Reason.INVALID_REQUEST
        else -> Reason.UNKNOWN
    }
    private fun fail(result: MethodChannel.Result, request: String, code: String,
                     stage: Stage, route: Route, failure: Reason = reason(code), androidError: Int? = null) {
        val numeric = androidError?.takeIf { it in 1..15 }
        diagnostic(stage, route, failure, numeric)
        result.error(code, "Voice session ended", mapOf(
            "schemaVersion" to "voice.input_failure.v1", "requestId" to request,
            "stage" to stage.wire, "route" to route.wire,
            "reasonClass" to failure.wire, "androidError" to numeric))
    }

    private fun failActivity(result: MethodChannel.Result, request: String, code: String,
                             stage: Stage, failure: Reason, resultCode: Int? = null) {
        val numeric = resultCode?.takeIf { it in 0..5 }
        if ((activity.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE) != 0) {
            Log.d("BirdtieVoiceInput", "stage=${stage.wire} route=system_activity reason=${failure.wire} activityResultCode=${numeric ?: "none"}")
        }
        result.error(code, "Voice activity ended", mapOf(
            "schemaVersion" to "voice.activity_failure.v1", "requestId" to request,
            "stage" to stage.wire, "route" to "system_activity",
            "reasonClass" to failure.wire, "activityResultCode" to numeric))
    }

    @Suppress("DEPRECATION")
    private fun systemService(): Service? = try {
        val info = activity.packageManager.resolveService(Intent(RecognitionService.SERVICE_INTERFACE), 0)?.serviceInfo
        if (info == null || !info.enabled || !info.exported || !info.applicationInfo.enabled) null
        else {
            val name = info.loadLabel(activity.packageManager).toString().trim()
            if (name.isEmpty() || name.length > 200) null
            else Service(ComponentName(info.packageName, info.name), name)
        }
    } catch (_: Exception) { null }

    private fun localAvailable(): Boolean = Build.VERSION.SDK_INT >= 31 &&
        try { SpeechRecognizer.isOnDeviceRecognitionAvailable(activity) } catch (_: Exception) { false }
    @Suppress("DEPRECATION")
    private fun systemActivity(): Service? = try {
        activity.packageManager.resolveActivity(Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH), PackageManager.MATCH_DEFAULT_ONLY)
            ?.activityInfo?.takeIf { info ->
                info.enabled && info.exported && info.applicationInfo.enabled &&
                    (info.permission == null || activity.checkSelfPermission(info.permission) == PackageManager.PERMISSION_GRANTED)
            }?.let { info ->
                val name = info.loadLabel(activity.packageManager).toString().trim()
                if (name.isEmpty() || name.length > 200) null
                else Service(ComponentName(info.packageName, info.name), name)
            }
    } catch (_: Exception) { null }
    private fun granted(): Boolean = activity.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
    private fun arguments(call: MethodCall, keys: Set<String>): Map<*, *>? {
        val args = call.arguments as? Map<*, *> ?: return null
        if (args.keys != keys) return null
        return args
    }
    private fun id(args: Map<*, *>?): String? = (args?.get("requestId") as? String)?.takeIf {
        it.length <= 80 && Regex("voice-[0-9]+-[0-9]+").matches(it)
    }
    private fun intent(language: String) = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
        putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
        if (language == "auto") {
            // Do not silently lock mixed speech to Chinese. These are requests
            // to the actual engine, not a claim that every engine supports it.
            if (Build.VERSION.SDK_INT >= 34) {
                putExtra(RecognizerIntent.EXTRA_ENABLE_LANGUAGE_DETECTION, true)
                putExtra(RecognizerIntent.EXTRA_ENABLE_LANGUAGE_SWITCH, RecognizerIntent.LANGUAGE_SWITCH_BALANCED)
                putStringArrayListExtra(RecognizerIntent.EXTRA_LANGUAGE_DETECTION_ALLOWED_LANGUAGES,
                    arrayListOf("zh-CN", "en-GB", "en-US"))
                putStringArrayListExtra(RecognizerIntent.EXTRA_LANGUAGE_SWITCH_ALLOWED_LANGUAGES,
                    arrayListOf("zh-CN", "en-GB", "en-US"))
            }
        } else {
            putExtra(RecognizerIntent.EXTRA_LANGUAGE, language)
        }
        putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 1)
        putExtra(RecognizerIntent.EXTRA_PARTIAL_RESULTS, false)
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        if (closed) { result.error("CANCELLED", "Voice owner closed", null); return }
        when (call.method) {
            "availability" -> {
                if (call.arguments != null) { result.error("INVALID", "Invalid voice probe", null); return }
                val system = systemService()
                val speechActivity = systemActivity()
                val local = bundled.capabilityDeclared || localAvailable()
                diagnostic(Stage.AVAILABILITY, if (local) Route.ON_DEVICE else if (speechActivity != null) Route.SYSTEM_ACTIVITY else if (system != null) Route.SYSTEM else Route.UNKNOWN,
                    if (local || system != null || speechActivity != null) Reason.AVAILABLE else Reason.UNSUPPORTED)
                result.success(mapOf("onDevice" to local, "bundledOffline" to bundled.capabilityDeclared, "systemService" to system?.let {
                    mapOf("id" to it.component.flattenToShortString(), "name" to it.name)
                }, "systemActivity" to speechActivity?.let {
                    mapOf("id" to it.component.flattenToShortString(), "name" to it.name)
                }))
            }
            "permission" -> requestPermission(call, result)
            "localSupport" -> checkSupport(call, result)
            "recognize" -> recognize(call, result)
            "recognizeActivity" -> recognizeActivity(call, result)
            "stop", "cancel" -> {
                val request = id(arguments(call, setOf("requestId")))
                if (request == null) { result.error("INVALID", "Invalid voice request", null); return }
                if (call.method == "cancel") retire(request)
                else {
                    bundled.stop(request)
                    active?.takeIf { it.id == request && !it.stopping }?.let { job ->
                        job.stopping = true
                        phase(job, "processing")
                        timeout(job, 10000L)
                        try { job.recognizer.stopListening() }
                        catch (error: Exception) { finish(job, "UNAVAILABLE", stage = Stage.STOP, failure = exceptionReason(error)) }
                    }
                }
                result.success(null)
            }
            else -> result.notImplemented()
        }
    }

    private fun requestPermission(call: MethodCall, result: MethodChannel.Result) {
        val request = id(arguments(call, setOf("requestId")))
        if (request == null) { result.error("INVALID", "Invalid voice request", null); return }
        if (!resumed) { result.error("CANCELLED", "Not in foreground", null); return }
        if (permission != null || external != null || active != null || support != null || bundled.busy) { result.error("BUSY", "Permission already pending", null); return }
        if (granted()) { result.success(mapOf("requestId" to request, "granted" to true)); return }
        if (permissionCode >= 65535) { fail(result, request, "UNAVAILABLE", Stage.PERMISSION, Route.UNKNOWN, Reason.RATE_LIMIT); return }
        val job = PermissionJob(request, ++permissionCode, result)
        permission = job
        job.timeout = Runnable { if (permission === job) finishPermission(job, false) }
        handler.postDelayed(job.timeout!!, 90000L)
        try { activity.requestPermissions(arrayOf(Manifest.permission.RECORD_AUDIO), job.code) }
        catch (_: Exception) { finishPermission(job, false) }
    }
    fun onPermissionsResult(code: Int, permissions: Array<out String>, results: IntArray) {
        val job = permission?.takeIf { it.code == code } ?: return
        job.granted = permissions.size == 1 && permissions[0] == Manifest.permission.RECORD_AUDIO &&
            results.size == 1 && results[0] == PackageManager.PERMISSION_GRANTED && granted()
        if (job.granted != true || resumed) finishPermission(job, job.granted == true)
    }
    private fun finishPermission(job: PermissionJob, allowed: Boolean) {
        if (permission !== job) return
        permission = null
        job.timeout?.let(handler::removeCallbacks)
        job.result.success(mapOf("requestId" to job.id, "granted" to allowed))
    }

    private fun checkSupport(call: MethodCall, result: MethodChannel.Result) {
        val args = arguments(call, setOf("requestId", "language"))
        val request = id(args)
        val language = args?.get("language") as? String
        if (request == null || language !in listOf("auto", "zh-CN", "en-GB")) {
            result.error("INVALID", "Invalid voice language", null); return
        }
        if (!resumed || !granted()) { result.error("PERMISSION", "Microphone permission required", null); return }
        if (support != null || active != null || external != null || permission != null || bundled.busy) { result.error("BUSY", "Voice already active", null); return }
        // The selected bundled engine never falls through to a network-capable
        // Android service when its asset or native library is unavailable.
        if (bundled.capabilityDeclared) { bundled.checkReady(request, result); return }
        if (Build.VERSION.SDK_INT < 33 || !localAvailable()) {
            diagnostic(Stage.SUPPORT, Route.ON_DEVICE, Reason.UNSUPPORTED)
            result.success(mapOf("requestId" to request, "ready" to false)); return
        }
        val recognizer = try { SpeechRecognizer.createOnDeviceSpeechRecognizer(activity) }
            catch (error: Exception) { diagnostic(Stage.CREATE, Route.ON_DEVICE, exceptionReason(error)); result.success(mapOf("requestId" to request, "ready" to false)); return }
        val job = SupportJob(request, recognizer, result)
        support = job
        job.timeout = Runnable { finishSupport(job, false, Reason.TIMEOUT) }
        handler.postDelayed(job.timeout!!, 5000L)
        try {
            recognizer.checkRecognitionSupport(intent(language!!), activity.mainExecutor, object : RecognitionSupportCallback {
                override fun onSupportResult(value: RecognitionSupport) {
                    // Only installed support is ready; pending/supported/online
                    // languages never authorize an on-device ready claim.
                    val installed = value.installedOnDeviceLanguages
                    val ready = if (language == "auto") {
                        Build.VERSION.SDK_INT >= 34 &&
                            installed.any { it.equals("zh-CN", ignoreCase = true) } &&
                            installed.any { it.equals("en-GB", ignoreCase = true) || it.equals("en-US", ignoreCase = true) }
                    } else installed.any { it.equals(language, ignoreCase = true) }
                    finishSupport(job, ready)
                }
                override fun onError(error: Int) { finishSupport(job, false, androidReason(error), error) }
            })
        } catch (error: Exception) { finishSupport(job, false, exceptionReason(error)) }
    }
    private fun finishSupport(job: SupportJob, ready: Boolean, failure: Reason? = null, androidError: Int? = null) {
        if (support !== job) return
        support = null
        job.timeout?.let(handler::removeCallbacks)
        job.recognizer.destroy()
        diagnostic(Stage.SUPPORT, Route.ON_DEVICE, failure ?: if (ready) Reason.AVAILABLE else Reason.LANGUAGE, androidError)
        job.result.success(mapOf("requestId" to job.id, "ready" to ready))
    }

    private fun recognize(call: MethodCall, result: MethodChannel.Result) {
        val args = arguments(call, setOf("requestId", "language", "onDevice", "serviceId", "serviceName", "systemApproved"))
            ?: run { result.error("INVALID", "Invalid voice request", null); return }
        val request = id(args)
        val language = args?.get("language") as? String
        val onDevice = args?.get("onDevice") as? Boolean
        if (request == null || language !in listOf("auto", "zh-CN", "en-GB") || onDevice == null || args["systemApproved"] !is Boolean) {
            result.error("INVALID", "Invalid voice request", null); return
        }
        val route = if (onDevice) Route.ON_DEVICE else Route.SYSTEM
        if (!resumed) { fail(result, request, "CANCELLED", Stage.LIFECYCLE, route); return }
        if (!granted()) { fail(result, request, "PERMISSION", Stage.PERMISSION, route); return }
        if (active != null || support != null || permission != null || external != null || bundled.busy) { fail(result, request, "BUSY", Stage.START, route); return }
        if (onDevice && bundled.capabilityDeclared) {
            if (args["serviceId"] != null || args["serviceName"] != null) {
                fail(result, request, "INVALID", Stage.CREATE, route, Reason.INVALID_REQUEST); return
            }
            bundled.start(request, result); return
        }
        val recognizer = try {
            if (onDevice) {
                if (!localAvailable() || args["serviceId"] != null || args["serviceName"] != null) {
                    fail(result, request, "UNAVAILABLE", Stage.CREATE, route, Reason.UNSUPPORTED); return
                }
                SpeechRecognizer.createOnDeviceSpeechRecognizer(activity)
            } else {
                val service = systemService()
                if (args["systemApproved"] != true || service == null ||
                    args["serviceId"] != service.component.flattenToShortString() || args["serviceName"] != service.name) {
                    fail(result, request, "SERVICE_CHANGED", Stage.CREATE, route); return
                }
                SpeechRecognizer.createSpeechRecognizer(activity, service.component)
            }
        } catch (error: Exception) { fail(result, request, "UNAVAILABLE", Stage.CREATE, route, exceptionReason(error)); return }
        val job = Job(request, recognizer, result, route)
        active = job
        recognizer.setRecognitionListener(object : RecognitionListener {
            override fun onReadyForSpeech(params: Bundle?) { phase(job, "listening") }
            override fun onBeginningOfSpeech() { phase(job, "listening") }
            override fun onRmsChanged(value: Float) {}
            override fun onBufferReceived(buffer: ByteArray?) {} // No recording file or forwarding.
            override fun onEndOfSpeech() {
                if (active !== job) return
                job.stopping = true; phase(job, "processing"); timeout(job, 10000L)
            }
            override fun onPartialResults(results: Bundle?) {} // Never write an unconfirmed partial draft.
            override fun onEvent(type: Int, params: Bundle?) {}
            override fun onResults(results: Bundle?) {
                val text = results?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)?.firstOrNull()
                if (text.isNullOrBlank()) finish(job, "NO_SPEECH")
                else if (text.length > 4096) finish(job, "INVALID")
                else finish(job, text = text)
            }
            override fun onError(error: Int) {
                finish(job, when (error) {
                    SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS -> "PERMISSION"
                    SpeechRecognizer.ERROR_NO_MATCH, SpeechRecognizer.ERROR_SPEECH_TIMEOUT -> "NO_SPEECH"
                    SpeechRecognizer.ERROR_RECOGNIZER_BUSY -> "BUSY"
                    SpeechRecognizer.ERROR_LANGUAGE_NOT_SUPPORTED, SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE -> "LANGUAGE"
                    else -> "UNAVAILABLE"
                }, failure = androidReason(error), androidError = error)
            }
        })
        timeout(job, 30000L)
        try { recognizer.startListening(intent(language!!)) }
        catch (error: Exception) { finish(job, "UNAVAILABLE", stage = Stage.START, failure = exceptionReason(error)) }
    }
    private fun phase(job: Job, state: String) {
        if (active === job && !closed && !(state == "listening" && job.stopping)) {
            channel.invokeMethod("phase", mapOf("requestId" to job.id, "state" to state))
        }
    }
    @Suppress("DEPRECATION")
    private fun recognizeActivity(call: MethodCall, result: MethodChannel.Result) {
        val args = arguments(call, setOf("requestId", "language", "activityId", "activityName", "systemApproved"))
            ?: run { result.error("INVALID", "Invalid voice activity request", null); return }
        val request = id(args)
        val language = args?.get("language") as? String
        if (request == null || language !in listOf("auto", "zh-CN", "en-GB") || args?.get("systemApproved") != true) {
            result.error("INVALID", "Invalid voice activity request", null); return
        }
        if (!resumed) { failActivity(result, request, "CANCELLED", Stage.LIFECYCLE, Reason.CANCELLED); return }
        if (external != null || active != null || support != null || permission != null || bundled.busy) {
            failActivity(result, request, "BUSY", Stage.START, Reason.BUSY); return
        }
        val candidate = systemActivity()
        if (candidate == null || args["activityId"] != candidate.component.flattenToShortString() || args["activityName"] != candidate.name) {
            failActivity(result, request, "SERVICE_CHANGED", Stage.CREATE, Reason.SERVICE_CHANGED); return
        }
        if (Build.VERSION.SDK_INT < 26) {
            failActivity(result, request, "UNAVAILABLE", Stage.CREATE, Reason.UNSUPPORTED); return
        }
        val displayId = callerDisplayId()
        if (displayId == null) {
            failActivity(result, request, "UNAVAILABLE", Stage.CREATE, Reason.INVALID_STATE); return
        }
        // Share the monotonic code allocator with this channel's permission
        // dialogs. Retired child results must never match a later gesture.
        if (permissionCode >= 65535) { failActivity(result, request, "UNAVAILABLE", Stage.CREATE, Reason.RATE_LIMIT); return }
        val job = ExternalJob(request, ++permissionCode, result, displayId)
        external = job
        timing(TimingStage.LEASE_REQUEST, job)
        job.timeout = Runnable { if (external === job) retireExternal(job, "TIMEOUT", Stage.TIMEOUT, Reason.TIMEOUT) }
        handler.postDelayed(job.timeout!!, 45000L)
        // Dart grants the lifecycle lease only to this exact live request. A
        // fire-and-forget phase event may arrive after onPause and is unsafe.
        channel.invokeMethod("externalLease", mapOf("requestId" to request), object : MethodChannel.Result {
            override fun success(value: Any?) {
                if (external !== job) return
                val ack = value as? Map<*, *>
                if (!resumed || ack == null || ack.keys != setOf("requestId", "accepted") || ack["requestId"] != request || ack["accepted"] != true) {
                    retireExternal(job, "CANCELLED", Stage.LIFECYCLE, Reason.CANCELLED); return
                }
                timing(TimingStage.LEASE_ACK, job)
                if (callerDisplayId() != job.displayId) {
                    retireExternal(job, "UNAVAILABLE", Stage.START, Reason.INVALID_STATE); return
                }
                // Recheck the disclosed component after the asynchronous lease.
                val fresh = systemActivity()
                if (fresh == null || fresh.component != candidate.component || fresh.name != candidate.name) {
                    retireExternal(job, "SERVICE_CHANGED", Stage.CREATE, Reason.SERVICE_CHANGED); return
                }
                try {
                    val options = ActivityOptions.makeBasic().setLaunchDisplayId(job.displayId).toBundle()
                    timing(TimingStage.DISPLAY_BOUND, job)
                    job.launched = true
                    timing(TimingStage.LAUNCH, job)
                    activity.startActivityForResult(intent(language!!).setComponent(candidate.component), job.code, options)
                    if (external === job) timing(TimingStage.LAUNCH_RETURN, job)
                } catch (_: ActivityNotFoundException) {
                    retireExternal(job, "UNAVAILABLE", Stage.START, Reason.UNSUPPORTED)
                } catch (error: Exception) {
                    retireExternal(job, "UNAVAILABLE", Stage.START, exceptionReason(error))
                }
            }
            override fun error(code: String, message: String?, details: Any?) { retireExternal(job, "CANCELLED", Stage.LIFECYCLE, Reason.CANCELLED) }
            override fun notImplemented() { retireExternal(job, "CANCELLED", Stage.LIFECYCLE, Reason.CANCELLED) }
        })
    }
    fun onActivityResult(code: Int, resultCode: Int, data: Intent?) {
        val job = external?.takeIf { it.code == code && it.launched && !it.received } ?: return
        job.received = true
        job.resultCode = resultCode
        job.failure = when (resultCode) {
            Activity.RESULT_OK -> try {
                val values = data?.getStringArrayListExtra(RecognizerIntent.EXTRA_RESULTS)
                val value = values?.takeIf { it.size in 1..20 }?.firstOrNull()
                if (value.isNullOrBlank() || value.length > 4096) Reason.INVALID_REQUEST
                else { job.text = value; null }
            } catch (_: Exception) { Reason.INVALID_REQUEST }
            Activity.RESULT_CANCELED -> Reason.CANCELLED
            RecognizerIntent.RESULT_NO_MATCH -> Reason.NO_SPEECH
            RecognizerIntent.RESULT_CLIENT_ERROR -> Reason.CLIENT
            RecognizerIntent.RESULT_SERVER_ERROR -> Reason.SERVICE
            RecognizerIntent.RESULT_NETWORK_ERROR -> Reason.NETWORK
            RecognizerIntent.RESULT_AUDIO_ERROR -> Reason.AUDIO
            else -> Reason.UNKNOWN
        }
        timing(TimingStage.RESULT, job, job.failure)
        if (resumed) finishExternal(job)
    }
    private fun finishExternal(job: ExternalJob) {
        if (external !== job || !job.received || !resumed || closed) return
        external = null
        job.timeout?.let(handler::removeCallbacks)
        val failure = job.failure
        timing(TimingStage.DELIVERED, job, failure)
        if (failure != null) {
            val code = when (failure) { Reason.CANCELLED -> "CANCELLED"; Reason.NO_SPEECH -> "NO_SPEECH"; Reason.INVALID_REQUEST -> "INVALID"; else -> "UNAVAILABLE" }
            failActivity(job.result, job.id, code, Stage.RECOGNITION, failure, job.resultCode)
        } else job.result.success(mapOf("requestId" to job.id, "text" to job.text))
        job.text = null
    }
    @Suppress("DEPRECATION")
    private fun retireExternal(job: ExternalJob, code: String, stage: Stage, failure: Reason) {
        if (external !== job) return
        timing(if (stage == Stage.TIMEOUT) TimingStage.TIMEOUT else TimingStage.RETIRED, job, failure)
        external = null // Retire before finishing only our owned child.
        job.timeout?.let(handler::removeCallbacks)
        job.text = null
        if (job.launched && !job.received) try { activity.finishActivity(job.code) } catch (_: Exception) {}
        failActivity(job.result, job.id, code, stage, failure)
    }
    private fun timeout(job: Job, millis: Long) {
        if (active !== job) return
        job.timeout?.let(handler::removeCallbacks)
        job.timeout = Runnable { finish(job, "TIMEOUT", stage = Stage.TIMEOUT) }
        handler.postDelayed(job.timeout!!, millis)
    }
    private fun finish(job: Job, error: String? = null, text: String? = null,
                       stage: Stage = Stage.RECOGNITION, failure: Reason? = null, androidError: Int? = null) {
        if (active !== job) return
        active = null // Retire before cancel/destroy can deliver another callback.
        job.timeout?.let(handler::removeCallbacks)
        try { job.recognizer.cancel() } catch (_: Exception) {}
        try { job.recognizer.destroy() } catch (_: Exception) {}
        if (error != null) fail(job.result, job.id, error, stage, job.route, failure ?: reason(error), androidError)
        else job.result.success(mapOf("requestId" to job.id, "text" to text))
    }
    private fun retire(id: String? = null) {
        bundled.cancel(id)
        external?.takeIf { id == null || it.id == id }?.let { retireExternal(it, "CANCELLED", Stage.LIFECYCLE, Reason.CANCELLED) }
        active?.takeIf { id == null || it.id == id }?.let { finish(it, "CANCELLED", stage = Stage.LIFECYCLE) }
        support?.takeIf { id == null || it.id == id }?.let { finishSupport(it, false, Reason.CANCELLED) }
        permission?.takeIf { id == null || it.id == id }?.let { finishPermission(it, false) }
    }
    fun onResume() {
        resumed = true
        bundled.onResume()
        external?.let { timing(TimingStage.HOST_RESUME, it) }
        external?.takeIf { it.received }?.let(::finishExternal)
        permission?.takeIf { it.granted != null }?.let { finishPermission(it, it.granted == true && granted()) }
    }
    fun onPause() {
        resumed = false
        bundled.onPause()
        external?.let { timing(TimingStage.HOST_PAUSE, it) }
        external?.takeIf { !it.launched }?.let { retireExternal(it, "CANCELLED", Stage.LIFECYCLE, Reason.CANCELLED) }
        active?.let { finish(it, "CANCELLED", stage = Stage.LIFECYCLE) }
        support?.let { finishSupport(it, false, Reason.CANCELLED) }
        // An owned permission dialog may pause Activity. No audio exists yet.
    }
    fun onStop() {
        resumed = false
        bundled.onPause()
        external?.let { timing(TimingStage.HOST_STOP, it) }
        // Only a successfully launched, owned external Activity may wait while
        // the host is stopped. Embedded recording keeps its original boundary.
        active?.let { finish(it, "CANCELLED", stage = Stage.LIFECYCLE) }
        support?.let { finishSupport(it, false, Reason.CANCELLED) }
        permission?.let { finishPermission(it, false) }
        external?.takeIf { !it.launched }?.let { retireExternal(it, "CANCELLED", Stage.LIFECYCLE, Reason.CANCELLED) }
    }
    fun close() {
        if (closed) return
        closed = true; resumed = false; retire(); bundled.close()
        channel.setMethodCallHandler(null)
    }
}
