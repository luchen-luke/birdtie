param(
    [string]$Device = 'c641566b',
    [int]$Cycles = 10,
    [int]$StartIndex = 1,
    [switch]$CaptureLast
)

$ErrorActionPreference='Stop'
if ($Device -notmatch '^[A-Za-z0-9._:-]+$' -or $Cycles -lt 1 -or $Cycles -gt 20) {
    throw 'Invalid device or cycle count.'
}
if ((& adb -s $Device get-state 2>$null) -ne 'device') { throw 'Android device is not connected.' }

function ComposerState {
    & adb -s $Device shell uiautomator dump /sdcard/birdtie-keyboard-check.xml 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect Birdtie UI.' }
    $raw = & adb -s $Device shell cat /sdcard/birdtie-keyboard-check.xml
    [xml]$document = $raw
    $edit = $document.SelectSingleNode('//node[@class="android.widget.EditText"]')
    $menu = $document.SelectSingleNode('//node[@content-desc="打开侧边栏"]')
    if (-not $edit -or -not $menu) { throw 'Birdtie Now page is no longer foreground.' }
    if ($edit.bounds -notmatch '^\[\d+,(\d+)\]\[\d+,\d+\]$') { throw 'Composer bounds unavailable.' }
    return [int]$Matches[1]
}

for ($i=0; $i -lt $Cycles; $i++) {
    $index=$StartIndex+$i
    $closedY=ComposerState
    if ($closedY -lt 2000) { throw "Cycle $index started with keyboard already open." }
    & adb -s $Device shell input tap 440 2450
    $openY=ComposerState
    if ($openY -ge 2000) {
        & adb -s $Device shell input tap 440 2450
        $openY=ComposerState
    }
    if ($openY -ge 2000) { throw "Cycle $index did not open the keyboard." }
    & adb -s $Device shell input text a
    if ($CaptureLast -and $i -eq $Cycles-1) {
        & adb -s $Device shell screencap -p /sdcard/birdtie-keyboard-open.png 2>$null | Out-Null
        & adb -s $Device pull /sdcard/birdtie-keyboard-open.png docs/research/evidence/2026-10-01-map-keyboard-open.png 2>$null | Out-Null
    }
    & adb -s $Device shell input keyevent KEYCODE_BACK
    $restoredY=ComposerState
    if ($restoredY -lt 2000) { throw "Cycle $index did not close only the keyboard." }
    Write-Output "[PASS] keyboard cycle $index"
}
if ($CaptureLast) {
    & adb -s $Device shell screencap -p /sdcard/birdtie-keyboard-closed.png 2>$null | Out-Null
    & adb -s $Device pull /sdcard/birdtie-keyboard-closed.png docs/research/evidence/2026-10-01-map-keyboard-closed.png 2>$null | Out-Null
}
