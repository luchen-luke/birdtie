package app.civu.civu_mobile

internal data class DestinationAdministrativeType(val featureType: String, val name: String)

private val municipalityCodes = setOf("110000", "120000", "310000", "500000")

/** Administrative search framing only; it never creates a City or location. */
internal fun destinationAdministrativeType(
    level: String?,
    adcode: String,
    province: String? = null,
    city: String? = null,
    district: String? = null,
    township: String? = null,
    country: String? = null,
    formattedAddress: String? = null,
): DestinationAdministrativeType? {
    // HK/MO/TW keep the worldwide provider boundary used by the existing bridge.
    if (!adcode.matches(Regex("[1-6][0-9]{5}"))) return null
    // AMap reports municipalities as province-level administrative areas. They
    // are city destinations for browsing; a district within one stays a district.
    val municipality = level == "省" && adcode in municipalityCodes
    val kind = when (level) {
        "国家" -> "country"
        "省" -> if (municipality) "place" else "region"
        "城市" -> "place"
        "区县" -> "district"
        "乡镇", "村庄" -> "locality"
        else -> return null
    }
    val label = when {
        municipality -> province.orEmpty().ifBlank { city.orEmpty() }
        kind == "region" -> province
        kind == "place" -> city
        kind == "district" -> district
        kind == "locality" -> township
        else -> country
    }.orEmpty().ifBlank { formattedAddress.orEmpty() }
    if (label.isBlank()) return null
    return DestinationAdministrativeType(kind, label)
}
