package content

// JourneysEnabled is a release boundary, not a user preference. Keep the
// existing private records and operation receipts readable while the Journey
// publishing/discovery surface is deferred. Moment/thread reads must not
// depend on migration 180 when this boundary is closed.
const JourneysEnabled = false
