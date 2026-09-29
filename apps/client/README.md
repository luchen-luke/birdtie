# Birdtie client

Flutter shell for Birdtie V2's Map Workspace + Agent + Sidebar + Inbox architecture. The home map fills the viewport, with a floating intent composer and a contextual result sheet. The first Agent task source is local UI state; People, Group and badminton map entities are labelled local previews. Published City, Place and Activity records still come from the Birdtie API. The prior Now and Explore UI remains in `lib/src/legacy/` during migration; the Sidebar exposes published city activities and the existing Profile/Moment draft surface. Public user content and the social graph remain disconnected.

## Public city data

Set `BIRDTIE_API_BASE_URL` when running the client. The selected City comes from `GET /v1/cities`, its Place list from `GET /v1/cities/{cityID}/places`, and published Activities from `GET /v1/cities/{cityID}/activities`; no device location is requested. The local task source filters already loaded published records by query text. It is not an Agent backend or authoritative search. Place results appear on the map only when the API permits public point precision. Activity records have no map coordinates in the current API, so they appear in the result sheet without invented markers. Configure the exact client origin in the API's `BIRDTIE_ALLOWED_ORIGINS` for Flutter web.

## Regional maps

The City API selects the map provider and initial display viewport. Aberdeen uses Mapbox. Web and native Android/iOS use the Civu-provided custom style `lookluo/cmth7kwad001p01ssc9kw7kco` in Birdtie-native adapters. Set `BIRDTIE_MAPBOX_PUBLIC_TOKEN` through `--dart-define-from-file=.env.maps.web.local.json` for Web; Static Tiles requires `styles:tiles`. Set `BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN` through `.env.maps.mobile.local.json` for native. The home map defaults to a few labelled local preview entities in Aberdeen; task results replace them and recenter the camera. Published Place markers only appear for task-matched public point records. The map does not use device location or private EXIF. Mapbox's logo and source links remain visible on the Web map; the native SDK provides its ornaments.

The existing Civu `MAPBOX_PUBLIC_TOKEN` is stored in ignored `apps/client/.env.maps.mobile.local.json` for native setup and has loaded the custom style on an Android phone. It returns 403 on Web Static Tiles because it lacks `styles:tiles`. Create a separate public Web token in the same Mapbox account and place it in ignored `apps/client/.env.maps.web.local.json`. The final app retains Civu's Android package `app.civu.civu_mobile` and iOS bundle ID `app.civu.civuMobile`; Android debug appends `.birdtiepreview` so both apps can be installed. The existing Civu Android AMap Key is in ignored `android/amap.properties` and is usable only when the package and release signing certificate match its registration. Domestic AMap's renderer and Web JS security proxy are still pending. The Civu Web Service Key is server-only. Exact provider console fields, local file paths and platform status are in `docs/architecture/MAP-PROVIDER-CONFIGURATION.md`.

Android release builds require the original Civu signing keystore through `BIRDTIE_ANDROID_STORE_FILE`, `BIRDTIE_ANDROID_KEY_ALIAS`, `BIRDTIE_ANDROID_STORE_PASSWORD` and `BIRDTIE_ANDROID_KEY_PASSWORD`. The build rejects an absent signer or a versionCode at or below the locally installed Civu baseline 2038. Check the highest published Play track before setting a final build number. Do not commit signing material. See `docs/migration/CIVU-TO-BIRDTIE-CUTOVER.md` for the full upgrade gates.

## Browser login

The web build can begin the Birdtie OIDC flow from Sidebar → Profile when the API reports that an identity provider is configured. The browser keeps the pending PKCE verifier in tab session storage until the redirect returns. The Birdtie Bearer session is held only in running memory, so a page reload requires another login. The login callback URL is cleared from browser history before code exchange.

The Profile surface lets signed-in users create, edit and withdraw own private Moment drafts. The client does not offer a publish control, media input, Place attachment or EXIF handling. Without a configured OIDC provider, these signed-in actions cannot yet be exercised with a real user.

Run the client with the API URL set at build time, for example:

```powershell
flutter run -d chrome --web-hostname=localhost --web-port=7357 --dart-define-from-file=.env.maps.web.local.json --dart-define=BIRDTIE_API_BASE_URL=http://127.0.0.1:8080
```

For a real login using the command above, allow `http://localhost:7357` through the API's `BIRDTIE_ALLOWED_ORIGINS` and set `BIRDTIE_OIDC_CLIENT_REDIRECT=http://localhost:7357/`. Register the API's `/v1/auth/oidc/callback` URL with the provider. Flutter's development port can vary, so keep it fixed when setting the redirect and origin. No issuer or client credentials are included in this repository. Native mobile and desktop login callbacks are not implemented.
