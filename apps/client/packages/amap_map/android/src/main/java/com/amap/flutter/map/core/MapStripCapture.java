package com.amap.flutter.map.core;

import android.graphics.Bitmap;
import android.graphics.Rect;
import android.os.Build;
import android.os.Handler;
import android.os.HandlerThread;
import android.os.Looper;
import android.view.PixelCopy;
import android.view.SurfaceView;
import android.view.View;
import android.view.ViewGroup;
import java.io.ByteArrayOutputStream;
import io.flutter.plugin.common.MethodChannel;

/** Reads a bounded region (up to the viewport) at 480px width; PNG encoding stays off the main thread. */
final class MapStripCapture {
    private static SurfaceView surface(View view) {
        if (view instanceof SurfaceView) return (SurfaceView) view;
        if (view instanceof ViewGroup) {
            ViewGroup group = (ViewGroup) view;
            for (int i = 0; i < group.getChildCount(); i++) {
                SurfaceView found = surface(group.getChildAt(i));
                if (found != null) return found;
            }
        }
        return null;
    }

    static void capture(View map, double fraction, MethodChannel.Result result) {
        SurfaceView view = surface(map);
        if (Build.VERSION.SDK_INT < 26 || view == null || view.getWidth() <= 0 || view.getHeight() <= 0
                || !view.getHolder().getSurface().isValid()) { result.success(null); return; }
        int width = view.getWidth(), height = view.getHeight();
        int stripHeight = Math.max(1, (int) Math.ceil(height * Math.max(.01, Math.min(1.0, fraction))));
        int outputWidth = Math.min(480, width);
        Bitmap target = Bitmap.createBitmap(outputWidth, Math.max(1, stripHeight * outputWidth / width), Bitmap.Config.ARGB_8888);
        HandlerThread worker = new HandlerThread("CivuMapStrip");
        worker.start();
        Handler main = new Handler(Looper.getMainLooper());
        try {
            PixelCopy.request(view, new Rect(0, height - stripHeight, width, height), target, status -> {
                byte[] bytes = null;
                try {
                    if (status == PixelCopy.SUCCESS) {
                        ByteArrayOutputStream stream = new ByteArrayOutputStream();
                        target.compress(Bitmap.CompressFormat.PNG, 100, stream);
                        bytes = stream.toByteArray();
                    }
                } finally {
                    target.recycle();
                    worker.quitSafely();
                }
                final byte[] output = bytes;
                main.post(() -> result.success(output));
            }, new Handler(worker.getLooper()));
        } catch (IllegalArgumentException | IllegalStateException error) {
            target.recycle();
            worker.quitSafely();
            result.success(null);
        }
    }
}
