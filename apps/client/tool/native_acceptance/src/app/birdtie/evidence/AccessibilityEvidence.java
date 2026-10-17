package app.birdtie.evidence;

import android.app.Instrumentation;
import android.app.UiAutomation;
import android.accessibilityservice.AccessibilityServiceInfo;
import android.graphics.Rect;
import android.view.Display;
import android.view.InputDevice;
import android.view.MotionEvent;
import android.os.Bundle;
import android.os.SystemClock;
import android.util.Base64;
import java.nio.charset.StandardCharsets;
import android.view.accessibility.AccessibilityEvent;
import android.view.accessibility.AccessibilityNodeInfo;
import android.view.accessibility.AccessibilityWindowInfo;
import java.util.List;
import org.json.JSONArray;
import org.json.JSONObject;
import java.util.concurrent.atomic.AtomicInteger;

/** External test APK, never packaged into Birdtie. Does not suppress TalkBack.
 * Observes only the two explicit preview IDs; text input is isolated-app only.
 * Its output is semantic evidence, not a claim that a human heard TTS speech.
 */
public final class AccessibilityEvidence extends Instrumentation {
    private Bundle args;
    private static final String ACCEPTANCE = "app.civu.civu_mobile.birdtieacceptance";
    private String target = ACCEPTANCE;
    @Override public void onCreate(Bundle arguments) { args = arguments; start(); }
    @Override public void onStart() {
        final Bundle result = new Bundle();
        try {
            target = args == null ? ACCEPTANCE : args.getString("target", ACCEPTANCE);
            if (!ACCEPTANCE.equals(target) && !"app.civu.civu_mobile.birdtiepreview".equals(target)) throw new IllegalArgumentException("Unknown target");
            UiAutomation automation = getUiAutomation(UiAutomation.FLAG_DONT_SUPPRESS_ACCESSIBILITY_SERVICES);
            AccessibilityServiceInfo service = automation.getServiceInfo();
            service.flags |= AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS;
            automation.setServiceInfo(service);
            SystemClock.sleep(250);
            AtomicInteger focusEvents = new AtomicInteger();
            AtomicInteger announcements = new AtomicInteger();
            JSONArray focused = new JSONArray();
            automation.setOnAccessibilityEventListener(event -> {
                if (!target.contentEquals(event.getPackageName() == null ? "" : event.getPackageName())) return;
                if (event.getEventType() == AccessibilityEvent.TYPE_VIEW_ACCESSIBILITY_FOCUSED) {
                    focusEvents.incrementAndGet();
                    AccessibilityNodeInfo source = event.getSource();
                    if (source != null) {
                        try { synchronized (focused) { if (focused.length() < 64) focused.put(node(source, -1)); } }
                        catch (Exception ignored) { }
                        source.recycle();
                    }
                }
                if (event.getEventType() == AccessibilityEvent.TYPE_ANNOUNCEMENT) announcements.incrementAndGet();
            });
            String action = args == null ? "dump" : args.getString("action", "dump");
            int duration = Math.min(30000, Math.max(0, args == null ? 0 : Integer.parseInt(args.getString("durationMs", "0"))));
            if ("observe".equals(action)) SystemClock.sleep(duration);
            AccessibilityNodeInfo root = null;
            List<AccessibilityWindowInfo> windows = automation.getWindowsOnAllDisplays().get(0);
            if (windows != null) for (AccessibilityWindowInfo window : windows) {
                if (!window.isFocused() || window.getType() != AccessibilityWindowInfo.TYPE_APPLICATION) continue;
                AccessibilityNodeInfo candidate = window.getRoot();
                if (candidate != null && target.contentEquals(candidate.getPackageName() == null ? "" : candidate.getPackageName())) {
                    root = candidate;
                    break;
                }
                if (candidate != null) candidate.recycle();
            }
            if (root == null || !target.contentEquals(root.getPackageName() == null ? "" : root.getPackageName())) {
                throw new IllegalStateException("OWN_WINDOW_NOT_INTERACTIVE_ON_DISPLAY_0");
            }
            JSONArray nodes = new JSONArray();
            int requested = Integer.parseInt(args == null ? "-1" : args.getString("node", "-1"));
            AtomicInteger index = new AtomicInteger();
            String value = args == null ? "" : new String(Base64.decode(args.getString("value64", ""), Base64.DEFAULT), StandardCharsets.UTF_8);
            if ("setText".equals(action) && !ACCEPTANCE.equals(target)) throw new IllegalArgumentException("Text input is isolated-app only");
            boolean performed = "swipe".equals(action) ? swipe(automation, root) : walk(root, nodes, index, requested, action, value);
            root.recycle();
            if (performed) SystemClock.sleep(700);
            automation.setOnAccessibilityEventListener(null);
            JSONObject output = new JSONObject();
            output.put("suppressAccessibilityServices", false);
            output.put("action", action);
            output.put("performed", performed);
            output.put("focusEvents", focusEvents.get());
            output.put("announcements", announcements.get());
            synchronized (focused) { output.put("focused", new JSONArray(focused.toString())); }
            output.put("nodes", nodes);
            output.put("speechComprehension", "NOT_OBSERVED");
            result.putString("evidence", output.toString());
            finish(0, result);
        } catch (Exception error) {
            result.putString("error", error.getClass().getSimpleName() + ": " + error.getMessage());
            finish(1, result);
        }
    }

    private boolean swipe(UiAutomation automation, AccessibilityNodeInfo root) {
        if (android.os.Build.VERSION.SDK_INT < 34) throw new IllegalStateException("Explicit display input requires API 34");
        int x1 = Integer.parseInt(args.getString("x1")), y1 = Integer.parseInt(args.getString("y1"));
        int x2 = Integer.parseInt(args.getString("x2")), y2 = Integer.parseInt(args.getString("y2"));
        int duration = Integer.parseInt(args.getString("durationMs", "600"));
        if (duration < 100 || duration > 6000) throw new IllegalArgumentException("Gesture duration out of range");
        Rect bounds = new Rect(); root.getBoundsInScreen(bounds);
        if (!bounds.contains(x1,y1) || !bounds.contains(x2,y2)) throw new IllegalArgumentException("Gesture outside current app");
        long down = SystemClock.uptimeMillis();
        boolean success = motion(automation, down, MotionEvent.ACTION_DOWN, x1, y1);
        int steps = Math.max(2, duration / 16);
        for (int i = 1; success && i < steps; i++) {
            SystemClock.sleep(Math.max(1, down + duration * i / steps - SystemClock.uptimeMillis()));
            success = motion(automation, down, MotionEvent.ACTION_MOVE, x1+(x2-x1)*(float)i/steps, y1+(y2-y1)*(float)i/steps);
        }
        SystemClock.sleep(Math.max(1, down + duration - SystemClock.uptimeMillis()));
        return motion(automation, down, success ? MotionEvent.ACTION_UP : MotionEvent.ACTION_CANCEL, x2,y2) && success;
    }

    private static boolean motion(UiAutomation automation, long down, int action, float x, float y) {
        MotionEvent.PointerProperties properties = new MotionEvent.PointerProperties();
        properties.id=0; properties.toolType=MotionEvent.TOOL_TYPE_FINGER;
        MotionEvent.PointerCoords coordinates = new MotionEvent.PointerCoords();
        coordinates.x=x; coordinates.y=y; coordinates.pressure=1f; coordinates.size=1f;
        MotionEvent event = MotionEvent.obtain(down,SystemClock.uptimeMillis(),action,1,
            new MotionEvent.PointerProperties[]{properties},new MotionEvent.PointerCoords[]{coordinates},
            0,0,1f,1f,0,0,InputDevice.SOURCE_TOUCHSCREEN,Display.DEFAULT_DISPLAY,0,MotionEvent.CLASSIFICATION_NONE);
        if(event==null) return false;
        try { return automation.injectInputEvent(event,true); } finally {event.recycle();}
    }

    private static JSONObject node(AccessibilityNodeInfo info, int index) throws Exception {
        JSONObject n = new JSONObject();
        Rect bounds = new Rect(); info.getBoundsInScreen(bounds);
        n.put("index", index);
        n.put("class", String.valueOf(info.getClassName()));
        n.put("text", info.isPassword() ? "[password]" : String.valueOf(info.getText()));
        n.put("description", String.valueOf(info.getContentDescription()));
        n.put("bounds", bounds.toShortString());
        n.put("clickable", info.isClickable());
        n.put("scrollable", info.isScrollable());
        n.put("enabled", info.isEnabled());
        n.put("focused", info.isAccessibilityFocused());
        n.put("actions", info.getActions());
        return n;
    }

    private static boolean walk(AccessibilityNodeInfo current, JSONArray nodes, AtomicInteger index,
                                int requested, String action, String value) throws Exception {
        int i = index.getAndIncrement();
        if (i >= 600) return false;
        nodes.put(node(current, i));
        boolean performed = false;
        if (i == requested) {
            if ("setText".equals(action) && current.isEditable() && !current.isPassword() && (current.getActions() & AccessibilityNodeInfo.ACTION_SET_TEXT) != 0) {
                Bundle text = new Bundle();
                text.putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, value);
                performed = current.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, text);
            }
            int code = "click".equals(action) ? AccessibilityNodeInfo.ACTION_CLICK
                : "focus".equals(action) ? AccessibilityNodeInfo.ACTION_ACCESSIBILITY_FOCUS
                : "scrollForward".equals(action) ? AccessibilityNodeInfo.ACTION_SCROLL_FORWARD
                : "scrollBackward".equals(action) ? AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD : 0;
            if (code != 0 && (current.getActions() & code) != 0) performed = current.performAction(code);
        }
        for (int child = 0; child < current.getChildCount(); child++) {
            AccessibilityNodeInfo childNode = current.getChild(child);
            if (childNode == null) continue;
            performed = walk(childNode, nodes, index, requested, action, value) || performed;
            childNode.recycle();
        }
        return performed;
    }
}
