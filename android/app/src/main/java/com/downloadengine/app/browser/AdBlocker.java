package com.downloadengine.app.browser;

import android.net.Uri;
import android.webkit.WebResourceResponse;

import java.io.ByteArrayInputStream;
import java.util.HashSet;
import java.util.Locale;
import java.util.Set;

public class AdBlocker {

    private static final Set<String> BLOCKED_HOSTS = new HashSet<>();

    static {
        // High-profile ad servers, popunder scripts, and mobile redirect networks
        String[] hosts = {
                "doubleclick.net", "googleads.g.doubleclick.net", "pagead2.googlesyndication.com",
                "adservice.google.com", "popads.net", "popcash.net", "propellerads.com",
                "exoclick.com", "adsterra.com", "adnxs.com", "revcontent.com",
                "mgid.com", "taboola.com", "outbrain.com", "clickadu.com",
                "hilltopads.net", "trafficstars.com", "ero-advertising.com",
                "juicyads.com", "admob.com", "criteo.com", "rubiconproject.com",
                "scorecardresearch.com", "quantserve.com", "zedo.com", "inmobi.com",
                "unityads.unity3d.com", "applovin.com", "chartboost.com", "vungle.com",
                "adcolony.com", "admob.google.com", "bidswitch.net", "smartadserver.com",
                "casalemedia.com", "openx.net", "pubmatic.com", "yieldmo.com"
        };
        for (String h : hosts) {
            BLOCKED_HOSTS.add(h.toLowerCase(Locale.US));
        }
    }

    private static boolean sEnabled = true;

    public static void setEnabled(boolean enabled) {
        sEnabled = enabled;
    }

    public static boolean isEnabled() {
        return sEnabled;
    }

    public static boolean isAd(String urlString) {
        if (!sEnabled || urlString == null) return false;
        try {
            Uri uri = Uri.parse(urlString);
            String host = uri.getHost();
            if (host == null) return false;
            host = host.toLowerCase(Locale.US);

            if (BLOCKED_HOSTS.contains(host)) return true;

            for (String blocked : BLOCKED_HOSTS) {
                if (host.endsWith("." + blocked)) {
                    return true;
                }
            }

            // Keyword pattern detection for ad tracking scripts
            String path = uri.getPath();
            if (path != null) {
                String pLower = path.toLowerCase(Locale.US);
                if (pLower.contains("/ads/") || pLower.contains("/popunder") ||
                    pLower.contains("/banner/") || pLower.contains("/adserver/")) {
                    return true;
                }
            }
        } catch (Exception ignored) {}
        return false;
    }

    public static WebResourceResponse createEmptyResource() {
        return new WebResourceResponse("text/plain", "UTF-8", new ByteArrayInputStream(new byte[0]));
    }
}
