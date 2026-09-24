#include "jni_bridge.h"
#include <stdlib.h>
#include <string.h>

static JavaVM *g_jvm = NULL;
static jmethodID g_midProgress = NULL;
static jmethodID g_midCompleted = NULL;
static jmethodID g_midError = NULL;

// Declarations of Go exported functions
extern char* goProbe(const char* rawURL, const char* customHeaders);
extern int goStartDownload(const char* taskId, const char* rawURL, const char* destPath, const char* statePath, int concurrency, int64_t chunkSize, int maxRetries, uintptr_t callbackHandle);
extern int goStartDownloadFD(const char* taskId, const char* rawURL, int fd, const char* statePath, int concurrency, int64_t chunkSize, int maxRetries, uintptr_t callbackHandle);
extern bool goPauseDownload(const char* taskId);
extern bool goCancelDownload(const char* taskId, const char* statePath, bool deleteFile, const char* destPath);
extern void freeGoCString(char* str);

JNIEXPORT jint JNICALL JNI_OnLoad(JavaVM *vm, void *reserved) {
    g_jvm = vm;
    JNIEnv *env = NULL;
    if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
        return JNI_ERR;
    }

    jclass callbackClass = (*env)->FindClass(env, "com/downloadengine/app/engine/NativeCallback");
    if (callbackClass != NULL) {
        g_midProgress = (*env)->GetMethodID(env, callbackClass, "onProgress", "(Ljava/lang/String;JJDDIII)V");
        g_midCompleted = (*env)->GetMethodID(env, callbackClass, "onCompleted", "(Ljava/lang/String;Ljava/lang/String;)V");
        g_midError = (*env)->GetMethodID(env, callbackClass, "onError", "(Ljava/lang/String;ILjava/lang/String;)V");
    }

    return JNI_VERSION_1_6;
}

static JNIEnv* getJNIEnv(int *attached) {
    if (attached != NULL) *attached = 0;
    if (g_jvm == NULL) return NULL;
    JNIEnv *env = NULL;
    jint res = (*g_jvm)->GetEnv(g_jvm, (void**)&env, JNI_VERSION_1_6);
    if (res == JNI_EDETACHED) {
        if ((*g_jvm)->AttachCurrentThread(g_jvm, &env, NULL) != 0) {
            return NULL;
        }
        if (attached != NULL) *attached = 1;
    }
    return env;
}

void notifyJavaProgress(uintptr_t callbackHandle, const char *taskId, int64_t downloaded, int64_t total, double speed, double eta, int activeWorkers, int completedChunks, int totalChunks) {
    if (callbackHandle == 0 || g_midProgress == NULL) return;
    int attached = 0;
    JNIEnv *env = getJNIEnv(&attached);
    if (env == NULL) return;

    jobject callback = (jobject)callbackHandle;
    jstring jTaskId = (*env)->NewStringUTF(env, taskId ? taskId : "");

    (*env)->CallVoidMethod(env, callback, g_midProgress, jTaskId, (jlong)downloaded, (jlong)total, (jdouble)speed, (jdouble)eta, (jint)activeWorkers, (jint)completedChunks, (jint)totalChunks);

    (*env)->DeleteLocalRef(env, jTaskId);
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
    if (attached) (*g_jvm)->DetachCurrentThread(g_jvm);
}

void notifyJavaCompleted(uintptr_t callbackHandle, const char *taskId, const char *destPath) {
    if (callbackHandle == 0 || g_midCompleted == NULL) return;
    int attached = 0;
    JNIEnv *env = getJNIEnv(&attached);
    if (env == NULL) return;

    jobject callback = (jobject)callbackHandle;
    jstring jTaskId = (*env)->NewStringUTF(env, taskId ? taskId : "");
    jstring jDest = (*env)->NewStringUTF(env, destPath ? destPath : "");

    (*env)->CallVoidMethod(env, callback, g_midCompleted, jTaskId, jDest);

    (*env)->DeleteLocalRef(env, jTaskId);
    (*env)->DeleteLocalRef(env, jDest);
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
    if (attached) (*g_jvm)->DetachCurrentThread(g_jvm);
}

void notifyJavaError(uintptr_t callbackHandle, const char *taskId, int errorCode, const char *errorMessage) {
    if (callbackHandle == 0 || g_midError == NULL) return;
    int attached = 0;
    JNIEnv *env = getJNIEnv(&attached);
    if (env == NULL) return;

    jobject callback = (jobject)callbackHandle;
    jstring jTaskId = (*env)->NewStringUTF(env, taskId ? taskId : "");
    jstring jMsg = (*env)->NewStringUTF(env, errorMessage ? errorMessage : "");

    (*env)->CallVoidMethod(env, callback, g_midError, jTaskId, (jint)errorCode, jMsg);

    (*env)->DeleteLocalRef(env, jTaskId);
    (*env)->DeleteLocalRef(env, jMsg);
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
    if (attached) (*g_jvm)->DetachCurrentThread(g_jvm);
}

void freeCallbackHandle(uintptr_t callbackHandle) {
    if (callbackHandle == 0) return;
    int attached = 0;
    JNIEnv *env = getJNIEnv(&attached);
    if (env == NULL) return;
    (*env)->DeleteGlobalRef(env, (jobject)callbackHandle);
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
    if (attached) (*g_jvm)->DetachCurrentThread(g_jvm);
}

// JNI exports called by NativeBridge.java
JNIEXPORT jstring JNICALL Java_com_downloadengine_app_engine_NativeBridge_nativeProbe(
    JNIEnv *env, jclass clazz, jstring jurl, jstring jheaders) {
    const char *url = jurl ? (*env)->GetStringUTFChars(env, jurl, NULL) : "";
    const char *headers = jheaders ? (*env)->GetStringUTFChars(env, jheaders, NULL) : "";

    char *jsonResult = goProbe(url, headers);

    if (jurl) (*env)->ReleaseStringUTFChars(env, jurl, url);
    if (jheaders) (*env)->ReleaseStringUTFChars(env, jheaders, headers);

    jstring res = (*env)->NewStringUTF(env, jsonResult ? jsonResult : "{}");
    if (jsonResult) {
        freeGoCString(jsonResult);
    }
    return res;
}

JNIEXPORT jint JNICALL Java_com_downloadengine_app_engine_NativeBridge_nativeStartDownload(
    JNIEnv *env, jclass clazz, jstring jtaskId, jstring jurl, jstring jdestPath, jstring jstatePath,
    jint concurrency, jlong chunkSize, jint maxRetries, jobject callback) {

    const char *taskId = jtaskId ? (*env)->GetStringUTFChars(env, jtaskId, NULL) : "";
    const char *url = jurl ? (*env)->GetStringUTFChars(env, jurl, NULL) : "";
    const char *destPath = jdestPath ? (*env)->GetStringUTFChars(env, jdestPath, NULL) : "";
    const char *statePath = jstatePath ? (*env)->GetStringUTFChars(env, jstatePath, NULL) : "";

    jobject globalCallback = callback ? (*env)->NewGlobalRef(env, callback) : NULL;

    int ret = goStartDownload(taskId, url, destPath, statePath, (int)concurrency, (int64_t)chunkSize, (int)maxRetries, (uintptr_t)globalCallback);

    if (jtaskId) (*env)->ReleaseStringUTFChars(env, jtaskId, taskId);
    if (jurl) (*env)->ReleaseStringUTFChars(env, jurl, url);
    if (jdestPath) (*env)->ReleaseStringUTFChars(env, jdestPath, destPath);
    if (jstatePath) (*env)->ReleaseStringUTFChars(env, jstatePath, statePath);

    return ret;
}

JNIEXPORT jint JNICALL Java_com_downloadengine_app_engine_NativeBridge_nativeStartDownloadFD(
    JNIEnv *env, jclass clazz, jstring jtaskId, jstring jurl, jint fd, jstring jstatePath,
    jint concurrency, jlong chunkSize, jint maxRetries, jobject callback) {

    const char *taskId = jtaskId ? (*env)->GetStringUTFChars(env, jtaskId, NULL) : "";
    const char *url = jurl ? (*env)->GetStringUTFChars(env, jurl, NULL) : "";
    const char *statePath = jstatePath ? (*env)->GetStringUTFChars(env, jstatePath, NULL) : "";

    jobject globalCallback = callback ? (*env)->NewGlobalRef(env, callback) : NULL;

    int ret = goStartDownloadFD(taskId, url, (int)fd, statePath, (int)concurrency, (int64_t)chunkSize, (int)maxRetries, (uintptr_t)globalCallback);

    if (jtaskId) (*env)->ReleaseStringUTFChars(env, jtaskId, taskId);
    if (jurl) (*env)->ReleaseStringUTFChars(env, jurl, url);
    if (jstatePath) (*env)->ReleaseStringUTFChars(env, jstatePath, statePath);

    return ret;
}

JNIEXPORT jboolean JNICALL Java_com_downloadengine_app_engine_NativeBridge_nativePause(
    JNIEnv *env, jclass clazz, jstring jtaskId) {
    const char *taskId = jtaskId ? (*env)->GetStringUTFChars(env, jtaskId, NULL) : "";
    bool ret = goPauseDownload(taskId);
    if (jtaskId) (*env)->ReleaseStringUTFChars(env, jtaskId, taskId);
    return ret ? JNI_TRUE : JNI_FALSE;
}

JNIEXPORT jboolean JNICALL Java_com_downloadengine_app_engine_NativeBridge_nativeCancel(
    JNIEnv *env, jclass clazz, jstring jtaskId, jstring jstatePath, jboolean deleteFile, jstring jdestPath) {
    const char *taskId = jtaskId ? (*env)->GetStringUTFChars(env, jtaskId, NULL) : "";
    const char *statePath = jstatePath ? (*env)->GetStringUTFChars(env, jstatePath, NULL) : "";
    const char *destPath = jdestPath ? (*env)->GetStringUTFChars(env, jdestPath, NULL) : "";

    bool ret = goCancelDownload(taskId, statePath, deleteFile == JNI_TRUE, destPath);

    if (jtaskId) (*env)->ReleaseStringUTFChars(env, jtaskId, taskId);
    if (jstatePath) (*env)->ReleaseStringUTFChars(env, jstatePath, statePath);
    if (jdestPath) (*env)->ReleaseStringUTFChars(env, jdestPath, destPath);

    return ret ? JNI_TRUE : JNI_FALSE;
}
