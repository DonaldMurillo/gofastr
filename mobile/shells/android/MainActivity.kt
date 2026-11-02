// NOT COMPILED: this container cannot download the Android SDK
// (dl.google.com is outside its network policy). This is the shape of the
// Android shell, written against the .aar `gomobile bind -target=android
// ./examples/notes/notesmobile` produces (Java package `notesmobile`).
// Verify on a device; the README lists what only a device can confirm.
package dev.gofastr.notes

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import notesmobile.Notesmobile
import java.io.ByteArrayInputStream
import java.io.InputStream

private const val APP_HOST = "appassets.androidplatform.net"
private const val ORIGIN = "https://$APP_HOST"

class MainActivity : Activity() {
    private lateinit var web: WebView

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        web = WebView(this)
        web.settings.javaScriptEnabled = true
        web.settings.domStorageEnabled = true
        web.webViewClient = GoClient()
        setContentView(web)
        // Migrations run in start: keep them off the main thread.
        Thread {
            Notesmobile.start(filesDir.absolutePath, ORIGIN)
            runOnUiThread { web.loadUrl("$ORIGIN/") }
        }.start()
    }

    override fun onDestroy() {
        Notesmobile.stop()
        super.onDestroy()
    }

    @Deprecated("Back gesture: hand it to the page's history (the client router) first.")
    override fun onBackPressed() {
        if (web.canGoBack()) web.goBack() else super.onBackPressed()
    }

    private inner class GoClient : WebViewClient() {
        // Runs on a WebView background thread, so a blocking call into Go
        // is fine here.
        override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
            if (request.url.host != APP_HOST) {
                return WebResourceResponse("text/plain", "utf-8", 403, "Forbidden", emptyMap(), ByteArrayInputStream(ByteArray(0)))
            }
            val headers = request.requestHeaders.entries.joinToString("\n") { "${it.key}: ${it.value}" }
            // WebResourceRequest exposes NO request body. A fetch() POST
            // reaches Go empty, which breaks every runtime RPC and form
            // save. See README: the fix is a body bridge (a JS shim that
            // hands bodies to a WebMessageListener keyed by a request id),
            // or a runtime transport hook in core-ui.
            val body = ByteArray(0)
            if (request.requestHeaders["Accept"]?.contains("text/event-stream") == true) {
                val s = Notesmobile.open(request.method, request.url.toString(), headers, body)
                return response(s.status.toInt(), s.headers, StreamInput(s))
            }
            val r = Notesmobile.handle(request.method, request.url.toString(), headers, body)
            // WebResourceResponse rejects 3xx statuses outright, so a
            // redirect must be followed before it gets here (README).
            return response(r.status.toInt(), r.headers, ByteArrayInputStream(r.body ?: ByteArray(0)))
        }

        override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
            if (request.url.host == APP_HOST) return false
            startActivity(Intent(Intent.ACTION_VIEW, request.url)) // external links leave the app
            return true
        }
    }

    private fun response(status: Int, headerLines: String, body: InputStream): WebResourceResponse {
        val headers = mutableMapOf<String, String>()
        for (line in headerLines.lineSequence()) {
            val i = line.indexOf(':')
            if (i > 0) headers[line.substring(0, i).trim()] = line.substring(i + 1).trim()
        }
        val type = headers["Content-Type"] ?: "application/octet-stream"
        val mime = type.substringBefore(';').trim()
        val charset = Regex("charset=([^;]+)").find(type)?.groupValues?.get(1)
        val reason = if (status in 200..299) "OK" else "Error"
        return WebResourceResponse(mime, charset, status, reason, headers, body)
    }

    /** Adapts a Go stream to the InputStream WebView pulls from. */
    private class StreamInput(private val s: notesmobile.Stream) : InputStream() {
        private var buf = ByteArray(0)
        private var pos = 0
        override fun read(): Int {
            if (!fill()) return -1
            return buf[pos++].toInt() and 0xff
        }
        override fun read(b: ByteArray, off: Int, len: Int): Int {
            if (!fill()) return -1
            val n = minOf(len, buf.size - pos)
            System.arraycopy(buf, pos, b, off, n)
            pos += n
            return n
        }
        private fun fill(): Boolean {
            while (pos >= buf.size) {
                buf = try { s.read(32 shl 10) ?: return false } catch (e: Exception) { return false } // io.EOF
                pos = 0
            }
            return true
        }
        override fun close() = s.close()
    }
}
