/// Explicit consent shared by map and geographic search for this app session.
/// It is never inferred from a query, location or account and is not persisted.
abstract final class AMapSessionConsent {
  static bool accepted = false;
}
