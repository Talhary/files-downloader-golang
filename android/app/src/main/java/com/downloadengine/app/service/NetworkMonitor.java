package com.downloadengine.app.service;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;

public class NetworkMonitor {

    public interface NetworkListener {
        void onNetworkChanged(boolean isConnected, boolean isWifi);
    }

    private final ConnectivityManager mConnMgr;
    private final NetworkListener mListener;
    private boolean mIsConnected = false;
    private boolean mIsWifi = false;

    public NetworkMonitor(Context context, NetworkListener listener) {
        mConnMgr = (ConnectivityManager) context.getSystemService(Context.CONNECTIVITY_SERVICE);
        mListener = listener;
        checkCurrentNetwork();
        registerCallback();
    }

    private void checkCurrentNetwork() {
        if (mConnMgr == null) return;
        Network active = mConnMgr.getActiveNetwork();
        if (active != null) {
            NetworkCapabilities caps = mConnMgr.getNetworkCapabilities(active);
            if (caps != null) {
                mIsConnected = caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET);
                mIsWifi = caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI);
            }
        }
    }

    private void registerCallback() {
        if (mConnMgr == null) return;
        NetworkRequest request = new NetworkRequest.Builder()
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .build();

        mConnMgr.registerNetworkCallback(request, new ConnectivityManager.NetworkCallback() {
            @Override
            public void onAvailable(Network network) {
                NetworkCapabilities caps = mConnMgr.getNetworkCapabilities(network);
                boolean isWifi = caps != null && caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI);
                mIsConnected = true;
                mIsWifi = isWifi;
                if (mListener != null) {
                    mListener.onNetworkChanged(true, isWifi);
                }
            }

            @Override
            public void onLost(Network network) {
                checkCurrentNetwork();
                if (mListener != null) {
                    mListener.onNetworkChanged(mIsConnected, mIsWifi);
                }
            }
        });
    }

    public boolean isConnected() { return mIsConnected; }
    public boolean isWifi() { return mIsWifi; }
}
