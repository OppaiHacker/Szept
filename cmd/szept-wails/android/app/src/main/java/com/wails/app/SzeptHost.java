package com.wails.app;

import android.content.ContentValues;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Environment;
import android.provider.MediaStore;
import android.util.Log;
import android.view.View;
import android.view.Window;
import android.webkit.JavascriptInterface;
import android.webkit.WebView;

import androidx.core.graphics.Insets;
import androidx.core.view.ViewCompat;
import androidx.core.view.WindowCompat;
import androidx.core.view.WindowInsetsCompat;
import androidx.core.view.WindowInsetsControllerCompat;

import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;

/**
 * What szept needs from Android beyond the stock Wails host, exposed to the
 * page as window.szeptHost: the microphone permission (voice runs in Go
 * through miniaudio, so the WebView never asks for it), handing staged files
 * to Downloads, system-bar insets and status-bar icon colour.
 */
final class SzeptHost {
    private static final String TAG = "SzeptHost";
    static final int MIC_REQUEST = 7021;
    private static final String MIC = "android.permission.RECORD_AUDIO";

    private final MainActivity activity;
    private final WebView web;
    // last system-bar insets in CSS px, re-read by the page after a reload
    private volatile String insets = "{\"top\":0,\"bottom\":0,\"left\":0,\"right\":0}";

    SzeptHost(MainActivity activity, WebView web) {
        this.activity = activity;
        this.web = web;
    }

    /**
     * Edge to edge on every API level (Android 15 forces it anyway). The page
     * paints under the status and navigation bars and pads itself from
     * --sa-* variables; the keyboard instead shrinks the WebView, so a
     * focused composer always sits right above it.
     */
    void installInsets(View container) {
        Window w = activity.getWindow();
        WindowCompat.setDecorFitsSystemWindows(w, false);
        w.setStatusBarColor(0);
        w.setNavigationBarColor(0);
        if (Build.VERSION.SDK_INT >= 29) w.setNavigationBarContrastEnforced(false);
        ViewCompat.setOnApplyWindowInsetsListener(container, (v, in) -> {
            Insets bars = in.getInsets(WindowInsetsCompat.Type.systemBars() | WindowInsetsCompat.Type.displayCutout());
            Insets ime = in.getInsets(WindowInsetsCompat.Type.ime());
            boolean kb = ime.bottom > bars.bottom;
            v.setPadding(0, 0, 0, kb ? ime.bottom : 0);
            float d = activity.getResources().getDisplayMetrics().density;
            insets = String.format(java.util.Locale.ROOT, "{\"top\":%.1f,\"bottom\":%.1f,\"left\":%.1f,\"right\":%.1f,\"kb\":%b}",
                    bars.top / d, kb ? 0f : bars.bottom / d, bars.left / d, bars.right / d, kb);
            emit("szept-insets", insets);
            return WindowInsetsCompat.CONSUMED;
        });
    }

    @JavascriptInterface
    public String insets() { return insets; }

    /** Dark icons on a light theme, light icons on the dark one. */
    @JavascriptInterface
    public void setLightBars(final boolean light) {
        activity.runOnUiThread(() -> {
            WindowInsetsControllerCompat c = WindowCompat.getInsetsController(activity.getWindow(), web);
            c.setAppearanceLightStatusBars(light);
            c.setAppearanceLightNavigationBars(light);
        });
    }

    @JavascriptInterface
    public boolean micGranted() {
        return activity.checkSelfPermission(MIC) == PackageManager.PERMISSION_GRANTED;
    }

    /** Asks once; the answer comes back as a "szept-mic" event. */
    @JavascriptInterface
    public void askMic() {
        activity.runOnUiThread(() -> activity.requestPermissions(new String[]{MIC}, MIC_REQUEST));
    }

    void micResult(boolean granted) { emit("szept-mic", granted ? "true" : "false"); }

    /**
     * Opens the camera QR scanner (linking a device). The text comes back as a
     * "szept-qr" event, {"text": ""} when cancelled.
     */
    @JavascriptInterface
    public void scanQR() {
        activity.runOnUiThread(() -> new com.google.zxing.integration.android.IntentIntegrator(activity)
                .setDesiredBarcodeFormats(com.google.zxing.integration.android.IntentIntegrator.QR_CODE)
                .setPrompt("Zeskanuj kod z ustawień, z sekcji Urządzenia")
                .setBeepEnabled(false)
                .setOrientationLocked(false)
                .initiateScan());
    }

    void qrResult(String text) {
        emit("szept-qr", "{\"text\":" + org.json.JSONObject.quote(text == null ? "" : text) + "}");
    }

    /**
     * Moves a file Go staged in filesDir/out to the public Downloads folder and
     * returns the name it got there ("" on failure). Only files in that one
     * directory are accepted: the page renders peer content, and this must not
     * become a way to copy the identity key out of the sandbox.
     */
    @JavascriptInterface
    public String exportFile(String path, String mime) {
        try {
            File out = new File(activity.getFilesDir(), "out").getCanonicalFile();
            File src = new File(path).getCanonicalFile();
            if (!out.equals(src.getParentFile()) || !src.isFile()) return "";
            String name = src.getName();
            if (Build.VERSION.SDK_INT >= 29) {
                ContentValues v = new ContentValues();
                v.put(MediaStore.MediaColumns.DISPLAY_NAME, name);
                if (mime != null && !mime.isEmpty()) v.put(MediaStore.MediaColumns.MIME_TYPE, mime);
                v.put(MediaStore.MediaColumns.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS + "/szept");
                Uri uri = activity.getContentResolver().insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, v);
                if (uri == null) return "";
                try (InputStream in = new FileInputStream(src);
                     OutputStream o = activity.getContentResolver().openOutputStream(uri)) {
                    copy(in, o);
                }
                name = "Pobrane/szept/" + name;
            } else {
                // before scoped storage: the app's own Downloads, no permission needed
                File dir = new File(activity.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS), "");
                dir.mkdirs();
                try (InputStream in = new FileInputStream(src);
                     OutputStream o = new FileOutputStream(new File(dir, name))) {
                    copy(in, o);
                }
                name = dir.getAbsolutePath() + "/" + name;
            }
            src.delete();
            return name;
        } catch (Exception e) {
            Log.e(TAG, "exportFile failed", e);
            return "";
        }
    }

    private static void copy(InputStream in, OutputStream o) throws java.io.IOException {
        byte[] buf = new byte[64 * 1024];
        int n;
        while ((n = in.read(buf)) > 0) o.write(buf, 0, n);
    }

    private void emit(String name, String detail) {
        web.post(() -> web.evaluateJavascript(
                "dispatchEvent(new CustomEvent('" + name + "',{detail:" + detail + "}))", null));
    }
}
