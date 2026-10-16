package com.amap.flutter.map;

/** Combines independent Activity and sheet ownership without recreating the map. */
final class MapRenderLifecycle {
    interface Delegate {
        void resume();
        void pause();
    }

    private final Delegate delegate;
    private boolean created;
    private boolean activityResumed;
    private boolean covered;
    private boolean running;
    private boolean transitionFailed;
    private boolean disposed;

    MapRenderLifecycle(Delegate delegate) {
        this.delegate = delegate;
    }

    void created() {
        if (disposed) return;
        created = true;
        reconcile();
    }

    void setActivityResumed(boolean value) {
        if (disposed) return;
        activityResumed = value;
        reconcile();
    }

    void setCovered(boolean value) {
        if (disposed) return;
        covered = value;
        reconcile();
    }

    void dispose() {
        if (disposed) return;
        disposed = true;
        try {
            if (running || transitionFailed) delegate.pause();
        } finally {
            running = false;
        }
    }

    private void reconcile() {
        boolean shouldRun = created && activityResumed && !covered;
        if (running == shouldRun && !transitionFailed) return;
        // Commit only after success so a repeated command can retry a failure.
        try {
            if (shouldRun) delegate.resume();
            else delegate.pause();
        } catch (RuntimeException e) {
            // SDK calls can partially apply before throwing. Do not suppress
            // a subsequent resume just because running still has its old value.
            transitionFailed = true;
            throw e;
        }
        running = shouldRun;
        transitionFailed = false;
    }
}
