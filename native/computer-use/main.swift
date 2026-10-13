// EasyAgent's native helper. Uses public macOS APIs; no MCP server or shell automation.
import AppKit
import ApplicationServices
import ScreenCaptureKit
import ImageIO
import UniformTypeIdentifiers

struct HelperError: Error, CustomStringConvertible {
    let description: String
    init(_ message: String) { description = message }
}
func emit(_ object: Any) throws {
    let data = try JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])
    FileHandle.standardOutput.write(data + Data([10]))
}
func requireAccess(screen: Bool = false) throws {
    guard AXIsProcessTrusted() else { throw HelperError("请在系统设置 → 隐私与安全性 → 辅助功能中授权 EasyAgent，然后刷新状态") }
    if screen && !CGPreflightScreenCaptureAccess() {
        throw HelperError("请在系统设置 → 隐私与安全性 → 屏幕录制中授权 EasyAgent，然后刷新状态")
    }
}
func arguments(_ args: [String]) throws -> [String: Any] {
    guard let index = args.firstIndex(of: "--args") else { return [:] }
    guard index + 1 < args.count, let data = args[index + 1].data(using: .utf8),
          let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
        throw HelperError("--args 必须是 JSON 对象")
    }
    return object
}
func integer(_ object: [String: Any], _ key: String, default fallback: Int? = nil) throws -> Int {
    guard let value = object[key] else {
        if let fallback = fallback { return fallback }
        throw HelperError("缺少 \(key)")
    }
    guard let number = value as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID(),
          number.doubleValue.isFinite, number.doubleValue.rounded() == number.doubleValue,
          abs(number.doubleValue) <= 1_000_000 else { throw HelperError("\(key) 必须是范围内的整数") }
    return number.intValue
}
let keyCodes: [String: CGKeyCode] = [
    "a":0,"s":1,"d":2,"f":3,"h":4,"g":5,"z":6,"x":7,"c":8,"v":9,"b":11,
    "q":12,"w":13,"e":14,"r":15,"y":16,"t":17,"1":18,"2":19,"3":20,"4":21,
    "6":22,"5":23,"=":24,"9":25,"7":26,"-":27,"8":28,"0":29,
    "]":30,"o":31,"u":32,"[":33,"i":34,"p":35,"enter":36,"l":37,"j":38,
    "'":39,"k":40,";":41,"\\":42,",":43,"/":44,"n":45,"m":46,".":47,
    "tab":48,"space":49,"`":50,"backspace":51,"esc":53,"delete":117,
    "left":123,"right":124,"down":125,"up":126,"home":115,"end":119,
    "pageup":116,"pagedown":121,"f1":122,"f2":120,"f3":99,"f4":118,
    "f5":96,"f6":97,"f7":98,"f8":100,"f9":101,"f10":109,"f11":103,"f12":111]
func key(_ combo: String) throws -> (CGKeyCode, CGEventFlags) {
    let parts = combo.lowercased().split(separator: "+", omittingEmptySubsequences: false).map(String.init)
    guard let last = parts.last, let code = keyCodes[last] else {
        throw HelperError("不支持的按键 \(combo)；输入文字请使用 type")
    }
    var flags: CGEventFlags = []
    for modifier in parts.dropLast() {
        switch modifier {
        case "cmd": flags.insert(.maskCommand)
        case "ctrl": flags.insert(.maskControl)
        case "alt": flags.insert(.maskAlternate)
        case "shift": flags.insert(.maskShift)
        case "fn": flags.insert(.maskSecondaryFn)
        default: throw HelperError("不支持的修饰键 \(modifier)")
        }
    }
    return (code, flags)
}
struct Target {
    let app: NSRunningApplication
    let window: SCWindow
    var bounds: CGRect { window.frame }
    var identity: [String: Any] {
        ["bundle_id": app.bundleIdentifier ?? "", "pid": app.processIdentifier, "window_id": window.windowID]
    }
}
func targetIdentity(_ input: [String: Any]) throws -> (String, pid_t, CGWindowID) {
    guard let bundle = input["bundle_id"] as? String, !bundle.isEmpty,
          let pid = input["pid"] as? NSNumber, CFGetTypeID(pid) != CFBooleanGetTypeID(),
          pid.doubleValue > 0, pid.doubleValue <= Double(Int32.max), pid.doubleValue.rounded() == pid.doubleValue,
          let window = input["window_id"] as? NSNumber, CFGetTypeID(window) != CFBooleanGetTypeID(),
          window.doubleValue > 0, window.doubleValue <= Double(UInt32.max), window.doubleValue.rounded() == window.doubleValue else {
        throw HelperError("请先 list_apps，再指定 bundle_id、pid 和 window_id；不会使用当前前台应用")
    }
    return (bundle, pid.int32Value, window.uint32Value)
}
func shareableContent() async throws -> SCShareableContent {
    try requireAccess(screen: true)
    return try await SCShareableContent.excludingDesktopWindows(true, onScreenWindowsOnly: false)
}
func resolveTarget(_ input: [String: Any]) async throws -> Target {
    let (bundle, pid, windowID) = try targetIdentity(input)
    guard let app = NSRunningApplication(processIdentifier: pid), app.bundleIdentifier == bundle, !app.isTerminated else {
        throw HelperError("目标应用已退出或身份改变，请重新 list_apps")
    }
    let content = try await shareableContent()
    guard let window = content.windows.first(where: { $0.windowID == windowID && $0.owningApplication?.processID == pid && $0.windowLayer == 0 }),
          window.frame.width > 0, window.frame.height > 0 else {
        throw HelperError("目标窗口已关闭或不可捕获，请重新 list_apps；不会改用其他窗口")
    }
    return Target(app: app, window: window)
}
func listApps() async throws -> [String: Any] {
    let content = try await shareableContent()
    var apps = [[String: Any]]()
    for app in NSWorkspace.shared.runningApplications where app.activationPolicy == .regular && !app.isTerminated {
        let windows = content.windows.filter { $0.owningApplication?.processID == app.processIdentifier && $0.windowLayer == 0 && $0.frame.width > 0 && $0.frame.height > 0 }
        if windows.isEmpty { continue }
        apps.append(["name": app.localizedName ?? "", "bundle_id": app.bundleIdentifier ?? "", "pid": app.processIdentifier,
                     "windows": windows.map { ["window_id": $0.windowID, "title": $0.title ?? "", "width": $0.frame.width, "height": $0.frame.height] as [String: Any] }])
    }
    return ["apps": apps, "coordinates": "target-window logical points, top-left origin"]
}
// Validate the whole batch before posting any event. The CLI can also validate without controlling a desktop.
func validateActions(_ input: [String: Any], bounds: [CGRect]) throws -> [[String: Any]] {
    guard let actions = input["actions"] as? [[String: Any]], !actions.isEmpty, actions.count <= 20 else {
        throw HelperError("动作批需要 1–20 个动作")
    }
    var waitTotal = 0
    for action in actions {
        switch action["type"] as? String {
        case "click", "double_click", "right_click":
            let point = CGPoint(x: try integer(action, "x"), y: try integer(action, "y"))
            guard bounds.contains(where: { $0.contains(point) }) else { throw HelperError("点击坐标超出目标窗口") }
        case "type":
            guard let text = action["text"] as? String, !text.isEmpty, text.unicodeScalars.count <= 2000 else {
                throw HelperError("type 需要 1–2000 个字符")
            }
        case "key":
            guard let combo = action["key"] as? String else { throw HelperError("key 需要按键名称") }
            _ = try key(combo)
        case "scroll":
            let dx = try integer(action, "dx", default: 0), dy = try integer(action, "dy", default: 0)
            guard abs(dx) <= 10000 && abs(dy) <= 10000 else { throw HelperError("滚动量过大") }
        case "wait":
            let ms = try integer(action, "ms", default: 0)
            guard ms >= 0 && ms <= 5000 else { throw HelperError("wait 范围为 0–5000ms") }
            waitTotal += ms
        default: throw HelperError("未知动作类型")
        }
    }
    guard waitTotal <= 50000 else { throw HelperError("单批累计等待不得超过 50 秒") }
    return actions
}
func post(_ event: CGEvent?, target: Target) throws {
    guard let event = event else { throw HelperError("无法创建键鼠事件") }
    if event.type != .keyDown && event.type != .keyUp { event.flags = [] }
    if event.type == .scrollWheel { event.location = CGPoint(x: target.bounds.midX, y: target.bounds.midY) }
    event.setIntegerValueField(.mouseEventWindowUnderMousePointer, value: Int64(target.window.windowID))
    event.setIntegerValueField(.mouseEventWindowUnderMousePointerThatCanHandleThisEvent, value: Int64(target.window.windowID))
    event.postToPid(target.app.processIdentifier)
}
func perform(_ actions: [[String: Any]], target: Target, window: AXUIElement) throws {
    for action in actions {
        // Permission may be revoked between steps; stop rather than claiming success.
        try requireAccess()
        guard !target.app.isTerminated, let rows = CGWindowListCopyWindowInfo([.optionIncludingWindow], target.window.windowID) as? [[String: Any]],
              rows.contains(where: { row in
                  guard (row[kCGWindowNumber as String] as? NSNumber)?.uint32Value == target.window.windowID,
                        (row[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == target.app.processIdentifier,
                        let raw = row[kCGWindowBounds as String] as? NSDictionary, let frame = CGRect(dictionaryRepresentation: raw) else { return false }
                  return frame == target.bounds
              }) else {
            throw HelperError("目标窗口已关闭或位置变化，动作批停止")
        }
        let type = action["type"] as! String
        switch type {
        case "click", "double_click", "right_click":
            let point = CGPoint(x: target.bounds.minX + Double(try integer(action, "x")), y: target.bounds.minY + Double(try integer(action, "y")))
            let right = type == "right_click"
            for index in 1...(type == "double_click" ? 2 : 1) {
                for kind: CGEventType in [right ? .rightMouseDown : .leftMouseDown, right ? .rightMouseUp : .leftMouseUp] {
                    let event = CGEvent(mouseEventSource: nil, mouseType: kind, mouseCursorPosition: point, mouseButton: right ? .right : .left)
                    event?.setIntegerValueField(.mouseEventClickState, value: Int64(index))
                    try post(event, target: target)
                }
                if type == "double_click" { Thread.sleep(forTimeInterval: 0.05) }
            }
        case "type":
            try requireKeyboardWindow(target, window: window)
            // AX replaces the target selection without the global clipboard or input focus.
            let root = AXUIElementCreateApplication(target.app.processIdentifier)
            if let raw = attribute(root, kAXFocusedUIElementAttribute), CFGetTypeID(raw) == AXUIElementGetTypeID() {
                let field = raw as! AXUIElement
                var settable = DarwinBoolean(false)
                if AXUIElementIsAttributeSettable(field, kAXSelectedTextAttribute as CFString, &settable) == .success && settable.boolValue {
                    guard AXUIElementSetAttributeValue(field, kAXSelectedTextAttribute as CFString, action["text"] as! CFString) == .success else {
                        throw HelperError("目标应用拒绝后台文字输入")
                    }
                    continue
                }
            }
            let units = Array((action["text"] as! String).utf16)
            // CGEvent limits Unicode payload length. Avoid splitting surrogate pairs.
            var start = 0
            while start < units.count {
                var end = min(start + 20, units.count)
                if end < units.count && units[end - 1] >= 0xD800 && units[end - 1] <= 0xDBFF { end -= 1 }
                let chunk = Array(units[start..<end])
                for down in [true, false] {
                    let event = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: down)
                    event?.flags = []
                    chunk.withUnsafeBufferPointer { event?.keyboardSetUnicodeString(stringLength: chunk.count, unicodeString: $0.baseAddress) }
                    try post(event, target: target)
                }
                start = end
            }
        case "key":
            try requireKeyboardWindow(target, window: window)
            let (code, flags) = try key(action["key"] as! String)
            for down in [true, false] {
                let event = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: down)
                event?.flags = flags
                try post(event, target: target)
            }
        case "scroll":
            try post(CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 2,
                             wheel1: Int32(try integer(action, "dy", default: 0)), wheel2: Int32(try integer(action, "dx", default: 0)), wheel3: 0), target: target)
        case "wait": Thread.sleep(forTimeInterval: Double(try integer(action, "ms", default: 0)) / 1000)
        default: throw HelperError("未知动作类型")
        }
    }
}
func attribute(_ element: AXUIElement, _ name: String) -> CFTypeRef? {
    var value: CFTypeRef?
    return AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success ? value : nil
}
func axFrame(_ node: AXUIElement) -> CGRect? {
    var point = CGPoint.zero, size = CGSize.zero
    guard let rawPoint = attribute(node, kAXPositionAttribute), CFGetTypeID(rawPoint) == AXValueGetTypeID(),
          AXValueGetValue(rawPoint as! AXValue, .cgPoint, &point),
          let rawSize = attribute(node, kAXSizeAttribute), CFGetTypeID(rawSize) == AXValueGetTypeID(),
          AXValueGetValue(rawSize as! AXValue, .cgSize, &size) else { return nil }
    return CGRect(origin: point, size: size)
}
func axWindow(_ target: Target) throws -> AXUIElement {
    let root = AXUIElementCreateApplication(target.app.processIdentifier)
    AXUIElementSetMessagingTimeout(root, 0.25)
    let windows = attribute(root, kAXWindowsAttribute) as? [AXUIElement] ?? []
    let matches = windows.filter { node in
        guard let frame = axFrame(node) else { return false }
        return abs(frame.minX - target.bounds.minX) < 2 && abs(frame.minY - target.bounds.minY) < 2 &&
            abs(frame.width - target.bounds.width) < 2 && abs(frame.height - target.bounds.height) < 2 &&
            (attribute(node, kAXTitleAttribute) as? String ?? "") == (target.window.title ?? "")
    }
    guard matches.count == 1 else { throw HelperError("无法唯一匹配目标窗口的辅助功能状态；请重新观察，不能退回前台控制") }
    return matches[0]
}
func requireKeyboardWindow(_ target: Target, window: AXUIElement) throws {
    let root = AXUIElementCreateApplication(target.app.processIdentifier)
    guard let focused = attribute(root, kAXFocusedWindowAttribute), CFGetTypeID(focused) == AXUIElementGetTypeID(),
          CFEqual(focused, window) else {
        throw HelperError("目标窗口不是该应用的键盘输入窗口，请先在目标窗口点击可编辑区域；不会输入到其他窗口")
    }
    if let raw = attribute(root, kAXFocusedUIElementAttribute), CFGetTypeID(raw) == AXUIElementGetTypeID() {
        let element = raw as! AXUIElement
        if attribute(element, kAXSubroleAttribute) as? String == "AXSecureTextField" || attribute(element, kAXRoleAttribute) as? String == "AXSecureTextField" {
            throw HelperError("不向密码输入框发送文字或按键")
        }
    }
}
func appState(_ target: Target) throws -> [String: Any] {
    try requireAccess()
    let root = try axWindow(target)
    var remaining = 150
    let deadline = Date().addingTimeInterval(4)
    func walk(_ node: AXUIElement, _ depth: Int) -> [String: Any] {
        remaining -= 1
        let role = attribute(node, kAXRoleAttribute) as? String ?? "unknown"
        let subrole = attribute(node, kAXSubroleAttribute) as? String ?? ""
        var result: [String: Any] = ["role": role]
        for name in [kAXTitleAttribute, kAXDescriptionAttribute] {
            if let value = attribute(node, name) as? String, !value.isEmpty { result[name] = String(value.prefix(500)) }
        }
        if role != "AXSecureTextField" && subrole != "AXSecureTextField", let value = attribute(node, kAXValueAttribute) as? String {
            result["value"] = String(value.prefix(1000))
        }
        var point = CGPoint.zero, size = CGSize.zero
        if let raw = attribute(node, kAXPositionAttribute), CFGetTypeID(raw) == AXValueGetTypeID(),
           AXValueGetValue(raw as! AXValue, .cgPoint, &point) {
            result["x"] = point.x - target.bounds.minX; result["y"] = point.y - target.bounds.minY
        }
        if let raw = attribute(node, kAXSizeAttribute), CFGetTypeID(raw) == AXValueGetTypeID(),
           AXValueGetValue(raw as! AXValue, .cgSize, &size) {
            result["width"] = size.width; result["height"] = size.height
        }
        if depth < 8, remaining > 0, Date() < deadline,
           let children = attribute(node, kAXChildrenAttribute) as? [AXUIElement] {
            var nodes = [[String: Any]]()
            for child in children where remaining > 0 && Date() < deadline { nodes.append(walk(child, depth + 1)) }
            result["children"] = nodes
        }
        return result
    }
    return ["target": target.identity, "app": target.app.localizedName ?? "",
            "coordinates": "target-window logical points, top-left origin", "state": walk(root, 0), "truncated": remaining <= 0 || Date() >= deadline]
}
@available(macOS 14.0, *)
func screenshot(_ input: [String: Any], target: Target) async throws -> [String: Any] {
    guard CGPreflightScreenCaptureAccess() else { throw HelperError("尚未授予屏幕录制权限，请在实验设置中请求系统权限") }
    guard let directory = input["output_dir"] as? String, directory.hasPrefix("/") else { throw HelperError("output_dir 必须是绝对路径") }
    let bounds = target.bounds
    let filter = SCContentFilter(desktopIndependentWindow: target.window)
    let config = SCStreamConfiguration()
    // Background window capture excludes occluding apps. Pixels equal logical window points.
    config.width = Int(bounds.width); config.height = Int(bounds.height)
    config.showsCursor = false; config.ignoreShadowsSingleWindow = true
    let image = try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: config)
    let url = URL(fileURLWithPath: directory).appendingPathComponent("window-\(UUID().uuidString).png")
    guard let output = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil) else { throw HelperError("无法写入截图") }
    CGImageDestinationAddImage(output, image, nil)
    guard CGImageDestinationFinalize(output) else { throw HelperError("截图写入失败") }
    try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    return ["path": url.path, "width": image.width, "height": image.height, "target": target.identity,
            "coordinates": "pixel = target-window logical point; top-left origin (0,0)"]
}
@MainActor
func run() async throws {
    let args = Array(CommandLine.arguments.dropFirst())
    switch args.first {
    case "--version": try emit(["protocol_version": 2, "version": Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") ?? "dev"])
    case "status":
        try emit(["accessibility": AXIsProcessTrusted(), "screen_recording": CGPreflightScreenCaptureAccess(), "protocol_version": 2])
    case "request-permissions":
        _ = AXIsProcessTrustedWithOptions([kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary)
        if !CGPreflightScreenCaptureAccess() { _ = CGRequestScreenCaptureAccess() }
        let pane = CGPreflightScreenCaptureAccess() ? "Privacy_Accessibility" : "Privacy_ScreenCapture"
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?\(pane)") else { throw HelperError("系统设置地址无效") }
        guard NSWorkspace.shared.open(url) else { throw HelperError("无法打开系统权限设置") }
        try emit(["success": true])
    case "validate-target":
        _ = try targetIdentity(arguments(args))
        try emit(["success": true])
    case "validate-actions":
        _ = try validateActions(arguments(args), bounds: [CGRect(x: 0, y: 0, width: 1920, height: 1080)])
        try emit(["success": true]) // Offline protocol tests; never posts events or asks for permissions.
    case "tool":
        guard args.count >= 2 else { throw HelperError("缺少 tool 动作") }
        let input = try arguments(args)
        switch args[1] {
        case "list_apps": try emit(try await listApps())
        case "screenshot": try emit(try await screenshot(input, target: resolveTarget(input)))
        case "app_state": try emit(try appState(await resolveTarget(input)))
        case "perform_action":
            let target = try await resolveTarget(input)
            let actions = try validateActions(input, bounds: [CGRect(origin: .zero, size: target.bounds.size)])
            let window = try axWindow(target)
            let foreground = NSWorkspace.shared.frontmostApplication?.processIdentifier
            let pointer = CGEvent(source: nil)?.location
            try perform(actions, target: target, window: window)
            var result: [String: Any] = ["success": true, "target": target.identity, "background": foreground != target.app.processIdentifier,
                                        "foreground_unchanged": foreground == NSWorkspace.shared.frontmostApplication?.processIdentifier,
                                        "pointer_unchanged": pointer == CGEvent(source: nil)?.location]
            if actions.contains(where: { $0["type"] as? String != "wait" }) { result["controlled_app"] = target.app.bundleIdentifier ?? "" }
            try emit(result)
        default: throw HelperError("未知 tool 动作")
        }
    default: throw HelperError("命令需要 status / request-permissions / tool")
    }
}
Task { @MainActor in
    do { try await run(); exit(0) }
    catch {
        let message = String(describing: error)
        try? emit(["success": false, "error": message])
        FileHandle.standardError.write(Data((message + "\n").utf8))
        exit(1)
    }
}
dispatchMain()
