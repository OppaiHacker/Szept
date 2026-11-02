package com.wails.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.os.Build;
import android.os.IBinder;

import androidx.annotation.Nullable;
import androidx.core.app.NotificationCompat;

/**
 * A minimal started foreground service. It does no work of its own — its purpose
 * is to keep the app's process alive (with the required ongoing notification) so
 * the developer's Go goroutines keep running while the app is backgrounded,
 * which Android would otherwise be free to kill. Start it from
 * {@link WailsBridge#startForegroundService(String)} and stop it with
 * {@link WailsBridge#stopForegroundService()}.
 */
public class WailsForegroundService extends android.app.Service {
    public static final String ACTION_START = "com.wails.app.FGS_START";
    private static final String CHANNEL_ID = "wails_foreground";
    private static final int NOTIFICATION_ID = 0x57A1; // "WAI"

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        // szept: keeps the process (and Go's link to the connector) alive so
        // messages arrive and notify while the screen is off
        String title = "szept";
        String text = "Odbiera wiadomości w tle";
        if (intent != null) {
            if (intent.getStringExtra("title") != null) title = intent.getStringExtra("title");
            if (intent.getStringExtra("text") != null) text = intent.getStringExtra("text");
        }
        // A sticky restart after the OS killed the process brings back only
        // this service: Go starts with the activity, so until it is opened
        // nothing is received. Say so instead of claiming otherwise.
        if (!MainActivity.goRunning()) text = "Wstrzymany. Dotknij, aby wznowić";

        NotificationManager nm = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel ch = new NotificationChannel(
                    CHANNEL_ID, "Działanie w tle", NotificationManager.IMPORTANCE_MIN);
            ch.setShowBadge(false);
            nm.createNotificationChannel(ch);
        }

        PendingIntent contentIntent = null;
        Intent launch = getPackageManager().getLaunchIntentForPackage(getPackageName());
        if (launch != null) {
            int piFlags = Build.VERSION.SDK_INT >= Build.VERSION_CODES.M
                    ? PendingIntent.FLAG_IMMUTABLE : 0;
            contentIntent = PendingIntent.getActivity(this, 0, launch, piFlags);
        }

        Notification n = new NotificationCompat.Builder(this, CHANNEL_ID)
                .setSmallIcon(android.R.drawable.stat_notify_chat)
                .setPriority(NotificationCompat.PRIORITY_MIN)
                .setContentTitle(title)
                .setContentText(text)
                .setOngoing(true)
                .setContentIntent(contentIntent)
                .build();

        // remoteMessaging (API 34+) has no daily cap; dataSync stops after 6h on Android 15
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(NOTIFICATION_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_REMOTE_MESSAGING);
        } else {
            startForeground(NOTIFICATION_ID, n);
        }
        // Restart if the OS kills us while still wanted.
        return START_STICKY;
    }

    @Nullable
    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}
