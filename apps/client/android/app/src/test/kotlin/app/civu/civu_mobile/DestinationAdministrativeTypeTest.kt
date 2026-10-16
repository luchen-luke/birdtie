package app.civu.civu_mobile

/** Direct JVM check of the production mapper; no Android SDK or network fixture. */
fun main() {
    val municipalities = mapOf(
        "110000" to "北京市", "120000" to "天津市",
        "310000" to "上海市", "500000" to "重庆市",
    )
    for ((code, name) in municipalities) {
        val value = destinationAdministrativeType("省", code, province = name, city = "市辖区")
        check(value == DestinationAdministrativeType("place", name)) { "municipality $code" }
    }
    check(destinationAdministrativeType("省", "310000", city = "上海市") ==
        DestinationAdministrativeType("place", "上海市")) { "municipality city fallback" }
    check(destinationAdministrativeType("省", "330000", province = "浙江省") ==
        DestinationAdministrativeType("region", "浙江省")) { "ordinary province" }
    check(destinationAdministrativeType("城市", "330100", province = "浙江省", city = "杭州市") ==
        DestinationAdministrativeType("place", "杭州市")) { "ordinary city" }
    check(destinationAdministrativeType("区县", "310101", province = "上海市", district = "黄浦区") ==
        DestinationAdministrativeType("district", "黄浦区")) { "municipal district must not be promoted" }
    check(destinationAdministrativeType("省", "310100", province = "上海市") ==
        DestinationAdministrativeType("region", "上海市")) { "no prefix-only municipality guess" }
    check(destinationAdministrativeType("道路", "310101", formattedAddress = "上海市道路") == null) { "not a destination" }
    check(destinationAdministrativeType("省", "810000", province = "香港") == null) { "provider boundary" }
    check(destinationAdministrativeType("省", "310000") == null) { "blank destination" }
    println("PASS: 12 administrative destination cases")
}
