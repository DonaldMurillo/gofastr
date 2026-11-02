// NOT COMPILED: this container has no Xcode. This is the shape of the iOS
// shell, written against the framework `gomobile bind -target=ios
// ./examples/notes/notesmobile` produces (Notesmobile.xcframework). Verify
// on a device before relying on any line of it; the README lists the
// platform behaviours that only a device can confirm.
import UIKit
import WebKit
import Notesmobile

/// The scheme the WebView loads the app under. WKURLSchemeHandler cannot
/// intercept http or https, so the iOS origin is a custom scheme; the Go
/// host is started with the same origin so it accepts these URLs.
let appScheme = "gofastr"
let appOrigin = "gofastr://app"

/// Serves every gofastr:// request from the in-process Go host.
final class GoSchemeHandler: NSObject, WKURLSchemeHandler {
    private let queue = DispatchQueue(label: "gofastr.scheme", attributes: .concurrent)
    private let lock = NSLock()
    private var live = Set<ObjectIdentifier>()

    func webView(_ webView: WKWebView, start task: WKURLSchemeTask) {
        let id = ObjectIdentifier(task)
        lock.withLock { _ = live.insert(id) }
        let req = task.request
        queue.async { [weak self, weak webView] in
            guard let self else { return }
            let method = req.httpMethod ?? "GET"
            let url = req.url?.absoluteString ?? ""
            let headers = (req.allHTTPHeaderFields ?? [:])
                .map { "\($0.key): \($0.value)" }.joined(separator: "\n")
            // VERIFY: WebKit has historically dropped POST bodies on custom
            // schemes. If httpBody and httpBodyStream are both nil for a
            // fetch() POST, the runtime's RPCs need a body bridge (README).
            let body = req.httpBody ?? Self.drain(req.httpBodyStream)

            if req.value(forHTTPHeaderField: "Accept")?.contains("text/event-stream") == true {
                self.stream(task, id: id, method: method, url: url, headers: headers, body: body)
                return
            }
            var err: NSError?
            guard let resp = NotesmobileHandle(method, url, headers, body, &err) else {
                self.finish(task, id: id) { task.didFailWithError(err ?? URLError(.cannotLoadFromNetwork)) }
                return
            }
            let status = Int(resp.status)
            let fields = Self.parse(resp.headers)
            // A scheme handler cannot return a redirect to WebKit. For a
            // top-level navigation, load the target; anything else fails.
            if (300..<400).contains(status), let loc = fields["Location"],
               let target = URL(string: loc, relativeTo: req.url) {
                self.finish(task, id: id) {
                    task.didFailWithError(URLError(.cancelled))
                    if req.mainDocumentURL == req.url { webView?.load(URLRequest(url: target)) }
                }
                return
            }
            guard let reqURL = req.url,
                  let http = HTTPURLResponse(url: reqURL, statusCode: status, httpVersion: "HTTP/1.1", headerFields: fields) else {
                self.finish(task, id: id) { task.didFailWithError(URLError(.badServerResponse)) }
                return
            }
            self.finish(task, id: id) {
                task.didReceive(http)
                task.didReceive(resp.body ?? Data())
                task.didFinish()
            }
        }
    }

    func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {
        // Calling any method on a stopped task raises; forget it.
        lock.withLock { _ = live.remove(ObjectIdentifier(task)) }
    }

    /// SSE and other streamed bodies: pull chunks from Go and hand each
    /// to WebKit as it arrives.
    private func stream(_ task: WKURLSchemeTask, id: ObjectIdentifier, method: String, url: String, headers: String, body: Data?) {
        var err: NSError?
        guard let s = NotesmobileOpen(method, url, headers, body, &err), let reqURL = task.request.url,
              let http = HTTPURLResponse(url: reqURL, statusCode: Int(s.status), httpVersion: "HTTP/1.1", headerFields: Self.parse(s.headers)) else {
            finish(task, id: id) { task.didFailWithError(err ?? URLError(.cannotLoadFromNetwork)) }
            return
        }
        guard deliver(id, { task.didReceive(http) }) else { try? s.close(); return }
        while true {
            var readErr: NSError?
            let chunk = s.read(32 << 10, error: &readErr)
            if let chunk, !chunk.isEmpty, !deliver(id, { task.didReceive(chunk) }) { try? s.close(); return }
            if readErr != nil { break } // io.EOF or a failure: either way the body ended
        }
        finish(task, id: id) { task.didFinish() }
    }

    /// Runs body on the main thread if the task is still live; reports
    /// whether it was.
    private func deliver(_ id: ObjectIdentifier, _ body: @escaping () -> Void) -> Bool {
        guard lock.withLock({ live.contains(id) }) else { return false }
        DispatchQueue.main.async { [weak self] in
            guard let self, self.lock.withLock({ self.live.contains(id) }) else { return }
            body()
        }
        return true
    }

    private func finish(_ task: WKURLSchemeTask, id: ObjectIdentifier, _ body: @escaping () -> Void) {
        DispatchQueue.main.async { [weak self] in
            guard let self, self.lock.withLock({ self.live.remove(id) != nil }) else { return }
            body()
        }
    }

    private static func parse(_ lines: String) -> [String: String] {
        var out: [String: String] = [:]
        for line in lines.split(separator: "\n") {
            guard let colon = line.firstIndex(of: ":") else { continue }
            let k = String(line[..<colon]).trimmingCharacters(in: .whitespaces)
            let v = String(line[line.index(after: colon)...]).trimmingCharacters(in: .whitespaces)
            out[k] = out[k].map { "\($0), \(v)" } ?? v
        }
        return out
    }

    private static func drain(_ stream: InputStream?) -> Data? {
        guard let stream else { return nil }
        stream.open(); defer { stream.close() }
        var data = Data()
        var buf = [UInt8](repeating: 0, count: 16 << 10)
        while stream.hasBytesAvailable {
            let n = stream.read(&buf, maxLength: buf.count)
            if n <= 0 { break }
            data.append(buf, count: n)
        }
        return data
    }
}

final class NotesViewController: UIViewController {
    private var webView: WKWebView!

    override func viewDidLoad() {
        super.viewDidLoad()
        let config = WKWebViewConfiguration()
        config.setURLSchemeHandler(GoSchemeHandler(), forURLScheme: appScheme)
        webView = WKWebView(frame: view.bounds, configuration: config)
        webView.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(webView)

        let dataDir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("app", isDirectory: true).path
        // Migrations run in Start: keep it off the main thread and leave the
        // launch screen up meanwhile.
        DispatchQueue.global(qos: .userInitiated).async {
            var err: NSError?
            let ok = NotesmobileStart(dataDir, appOrigin, &err)
            DispatchQueue.main.async {
                if ok { self.webView.load(URLRequest(url: URL(string: appOrigin + "/")!)) }
                else { NSLog("notes: start failed: %@", err?.localizedDescription ?? "unknown") }
            }
        }
    }
}
