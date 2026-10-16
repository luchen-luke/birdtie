package app.civu.civu_mobile

import android.content.Context
import android.os.Handler
import android.os.Looper
import com.amap.api.services.core.ServiceSettings
import com.amap.api.services.geocoder.GeocodeQuery
import com.amap.api.services.geocoder.GeocodeResult
import com.amap.api.services.geocoder.GeocodeSearch
import com.amap.api.services.geocoder.RegeocodeResult
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel

/** Bounded geographic search only. No device location or POI/database writes. */
class DestinationSearch(private val context: Context, messenger: BinaryMessenger) : MethodChannel.MethodCallHandler {
    private val channel = MethodChannel(messenger, "birdtie/destination_search")
    private val handler = Handler(Looper.getMainLooper())
    private data class Job(val id: String, val query: String, var reply: MethodChannel.Result?)
    private var active: Job? = null
    private var queued: Job? = null
    private var closed = false
    init { channel.setMethodCallHandler(this) }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        if (call.method == "cancel") {
            val id = call.argument<String>("requestId")
            if (active?.id == id) retire(active, "cancelled")
            if (queued?.id == id) { retire(queued, "cancelled"); queued = null }
            result.success(null)
            return
        }
        if (call.method != "search") { result.notImplemented(); return }
        val query = call.argument<String>("query")?.trim().orEmpty()
        val id = call.argument<String>("requestId").orEmpty()
        if (closed || call.argument<Boolean>("consent") != true || query.isEmpty() || query.length > 256 || id.isEmpty()) {
            result.error("unavailable", "地理搜索暂不可用", null); return
        }
        val job = Job(id, query, result)
        // The SDK has no cancellation primitive. Keep at most one SDK request
        // and one latest pending query; cancelled replies can never reach Dart.
        if (active != null) {
            retire(queued, "cancelled")
            queued = job
        } else start(job)
    }

    private fun retire(job: Job?, code: String) {
        job?.reply?.error(code, "地理搜索未完成，请重试", null)
        job?.reply = null
    }

    private fun start(job: Job) {
        if (closed) { retire(job, "closed"); return }
        active = job
        val timeout = Runnable {
            if (active === job) {
                retire(job, "timeout")
                retire(queued, "timeout")
                queued = null
                // Do not create a second SDK request while this one is alive.
            }
        }
        handler.postDelayed(timeout, 12000)
        try {
            ServiceSettings.updatePrivacyShow(context, true, true)
            ServiceSettings.updatePrivacyAgree(context, true)
            val search = GeocodeSearch(context)
            search.setOnGeocodeSearchListener(object : GeocodeSearch.OnGeocodeSearchListener {
                override fun onRegeocodeSearched(value: RegeocodeResult?, code: Int) = Unit
                override fun onGeocodeSearched(value: GeocodeResult?, code: Int) {
                    handler.post {
                        handler.removeCallbacks(timeout)
                        if (closed || active !== job) return@post
                        if (code != 1000 || value == null) {
                            // Numeric SDK codes only; no exception text, Key or URL.
                            job.reply?.error("search_failed", "国内目的地搜索失败", code)
                        } else {
                            val rows = value.geocodeAddressList.orEmpty().mapNotNull { address ->
                                val point = address.latLonPoint ?: return@mapNotNull null
                                val adcode = address.adcode.orEmpty()
                                val destination = destinationAdministrativeType(
                                    level = address.level, adcode = adcode,
                                    province = address.province, city = address.city,
                                    district = address.district, township = address.township,
                                    country = address.country, formattedAddress = address.formatAddress,
                                ) ?: return@mapNotNull null
                                val kind = destination.featureType
                                val name = destination.name
                                mapOf(
                                    "id" to "$adcode:$kind:$name", "name" to name,
                                    "region" to listOf(address.province, address.city, "中国").filterNot { it.isNullOrBlank() }.distinct().joinToString(" · "),
                                    "countryCode" to "CN", "featureType" to kind,
                                    "coordinateSystem" to "gcj02",
                                    "latitude" to point.latitude, "longitude" to point.longitude
                                )
                            }.distinctBy { it["id"] }.take(5)
                            job.reply?.success(mapOf("items" to rows, "attribution" to "高德地图"))
                        }
                        job.reply = null
                        active = null
                        val next = queued
                        queued = null
                        if (next != null) start(next)
                    }
                }
            })
            // No current city, device location or viewport restriction.
            search.getFromLocationNameAsyn(GeocodeQuery(job.query, ""))
        } catch (_: Exception) {
            handler.removeCallbacks(timeout)
            retire(job, "search_unavailable")
            active = null
            val next = queued
            queued = null
            if (next != null) start(next)
        }
    }

    fun close() {
        closed = true
        channel.setMethodCallHandler(null)
        handler.removeCallbacksAndMessages(null)
        retire(active, "closed"); retire(queued, "closed")
        active = null; queued = null
    }
}
