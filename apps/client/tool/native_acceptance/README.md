# Native acceptance evidence

This separate instrumentation APK runs in its own helper process and accepts
only `app.civu.civu_mobile.birdtieacceptance` and
`app.civu.civu_mobile.birdtiepreview` as semantic roots. The default is the
isolated acceptance package; `target` explicitly selects the normal preview.
Preview semantics can contain real user data and must stay in local ignored
evidence. It is not an app dependency and is not
included in preview or store builds. Build the app in profile mode with
`BIRDTIE_ANDROID_ACCEPTANCE_APP=1` and an explicit loopback API origin, and sign
this helper with the same local debug key.

The helper requests `FLAG_DONT_SUPPRESS_ACCESSIBILITY_SERVICES`, so an enabled
TalkBack service remains active. `am instrument -w -e action observe -e durationMs
20000 app.birdtie.evidence/app.birdtie.evidence.AccessibilityEvidence` observes real
accessibility focus events during gestures. The default `dump` action reports
the app's semantic tree; `focus`, `click`, `scrollForward`, `scrollBackward` accept
the bounded traversal `node` index from the immediately preceding dump. Password
values are never editable by this helper. `setText` accepts UTF-8 text through a
Base64 `value64` argument and only acts on a non-password editable node. Password
values are omitted. Text editing is restricted to the isolated application.
Save outputs outside
Git. Restore the device's exact accessibility settings after acceptance.
Verify the Birdtie process stays the same. Instrumentation restarts only the
helper package, never the observed Birdtie application. Do not use `--no-restart`
when the helper has no running process: some Android builds reject that request.

Semantic focus, reachable controls and activated actions are functional evidence.
This helper does not record audio or video and does not assert human speech
comprehension. The profile app's separately armed `birdtie.qa.mapSurfaceEvidence`
intent extra reports bounded numeric samples of the native map source buffer.
Those samples are `SOURCE_ONLY`, not proof of screen presentation, and flat color
does not by itself establish a blank map.

The optional `swipe` action uses the public API 34 MotionEvent overload with
explicit display 0 and a bounded duration of 100–6000 ms. It requires the observed
app to be the focused application window on display 0. Device input policy can
still reject it; record the rejection rather than claiming a gesture ran.
