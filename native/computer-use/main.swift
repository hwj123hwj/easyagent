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
func displayBounds() -> [CGRect] {
    var count: UInt32 = 0
    CGGetActiveDisplayList(0, nil, &count)
    var displays = [CGDirectDisplayID](repeating: 0, count: Int(count))
    CGGetActiveDisplayList(count, &displays, &count)
    return displays.prefix(Int(count)).map { CGDisplayBounds($0) }
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
            guard bounds.contains(where: { $0.contains(point) }) else { throw HelperError("点击坐标超出已连接屏幕") }
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
func post(_ event: CGEvent?) throws {
    guard let event = event else { throw HelperError("无法创建键鼠事件") }
    event.post(tap: .cghidEventTap)
}
func perform(_ actions: [[String: Any]]) throws {
    for action in actions {
        // Permission may be revoked between steps; stop rather than claiming success.
        try requireAccess()
        let type = action["type"] as! String
        switch type {
        case "click", "double_click", "right_click":
            let point = CGPoint(x: try integer(action, "x"), y: try integer(action, "y"))
            let right = type == "right_click"
            try post(CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: point, mouseButton: .left))
            for index in 1...(type == "double_click" ? 2 : 1) {
                for kind: CGEventType in [right ? .rightMouseDown : .leftMouseDown, right ? .rightMouseUp : .leftMouseUp] {
                    let event = CGEvent(mouseEventSource: nil, mouseType: kind, mouseCursorPosition: point, mouseButton: right ? .right : .left)
                    event?.setIntegerValueField(.mouseEventClickState, value: Int64(index))
                    try post(event)
                }
                if type == "double_click" { Thread.sleep(forTimeInterval: 0.05) }
            }
        case "type":
            let units = Array((action["text"] as! String).utf16)
            // CGEvent limits Unicode payload length. Avoid splitting surrogate pairs.
            var start = 0
            while start < units.count {
                var end = min(start + 20, units.count)
                if end < units.count && units[end - 1] >= 0xD800 && units[end - 1] <= 0xDBFF { end -= 1 }
                let chunk = Array(units[start..<end])
                for down in [true, false] {
                    let event = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: down)
                    chunk.withUnsafeBufferPointer { event?.keyboardSetUnicodeString(stringLength: chunk.count, unicodeString: $0.baseAddress) }
                    try post(event)
                }
                start = end
            }
        case "key":
            let (code, flags) = try key(action["key"] as! String)
            for down in [true, false] {
                let event = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: down)
                event?.flags = flags
                try post(event)
            }
        case "scroll":
            try post(CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 2,
                             wheel1: Int32(try integer(action, "dy", default: 0)), wheel2: Int32(try integer(action, "dx", default: 0)), wheel3: 0))
        case "wait": Thread.sleep(forTimeInterval: Double(try integer(action, "ms", default: 0)) / 1000)
        default: throw HelperError("未知动作类型")
        }
    }
}
func attribute(_ element: AXUIElement, _ name: String) -> CFTypeRef? {
    var value: CFTypeRef?
    return AXUIElementCopyAttributeValue(element, name as CFString, &value) == .success ? value : nil
}
func appState() throws -> [String: Any] {
    try requireAccess()
    guard let app = NSWorkspace.shared.frontmostApplication else { throw HelperError("没有前台应用") }
    let root = AXUIElementCreateApplication(app.processIdentifier)
    AXUIElementSetMessagingTimeout(root, 0.25)
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
            result["x"] = point.x; result["y"] = point.y
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
    return ["app": app.localizedName ?? "", "bundle_id": app.bundleIdentifier ?? "", "pid": app.processIdentifier,
            "coordinates": "global screen points, top-left origin", "state": walk(root, 0), "truncated": remaining <= 0 || Date() >= deadline]
}
@available(macOS 14.0, *)
func screenshot(_ input: [String: Any]) async throws -> [String: Any] {
    guard CGPreflightScreenCaptureAccess() else { throw HelperError("尚未授予屏幕录制权限，请在实验设置中请求系统权限") }
    guard let directory = input["output_dir"] as? String, directory.hasPrefix("/") else { throw HelperError("output_dir 必须是绝对路径") }
    let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: true)
    guard let display = content.displays.first(where: { $0.displayID == CGMainDisplayID() }) else { throw HelperError("找不到主显示器") }
    let bounds = CGDisplayBounds(display.displayID)
    let filter = SCContentFilter(display: display, excludingWindows: [])
    let config = SCStreamConfiguration()
    // One output pixel equals one logical point, including Retina/scaled displays.
    config.width = Int(bounds.width); config.height = Int(bounds.height); config.showsCursor = true
    let image = try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: config)
    let url = URL(fileURLWithPath: directory).appendingPathComponent("screen-\(UUID().uuidString).png")
    guard let output = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil) else { throw HelperError("无法写入截图") }
    CGImageDestinationAddImage(output, image, nil)
    guard CGImageDestinationFinalize(output) else { throw HelperError("截图写入失败") }
    try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
    return ["path": url.path, "width": image.width, "height": image.height, "x": bounds.minX, "y": bounds.minY,
            "coordinates": "pixel = screen point; add x/y to image coordinates; primary display only"]
}
@MainActor
func run() async throws {
    let args = Array(CommandLine.arguments.dropFirst())
    switch args.first {
    case "--version": try emit(["protocol_version": 1, "version": Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") ?? "dev"])
    case "status":
        try emit(["accessibility": AXIsProcessTrusted(), "screen_recording": CGPreflightScreenCaptureAccess(), "protocol_version": 1])
    case "request-permissions":
        _ = AXIsProcessTrustedWithOptions([kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary)
        if !CGPreflightScreenCaptureAccess() { _ = CGRequestScreenCaptureAccess() }
        let pane = CGPreflightScreenCaptureAccess() ? "Privacy_Accessibility" : "Privacy_ScreenCapture"
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?\(pane)") else { throw HelperError("系统设置地址无效") }
        guard NSWorkspace.shared.open(url) else { throw HelperError("无法打开系统权限设置") }
        try emit(["success": true])
    case "validate-actions":
        _ = try validateActions(arguments(args), bounds: [CGRect(x: 0, y: 0, width: 1920, height: 1080)])
        try emit(["success": true]) // Offline protocol tests; never posts events or asks for permissions.
    case "tool":
        guard args.count >= 2 else { throw HelperError("缺少 tool 动作") }
        let input = try arguments(args)
        switch args[1] {
        case "screenshot": try emit(try await screenshot(input))
        case "app_state": try emit(try appState())
        case "perform_action":
            let actions = try validateActions(input, bounds: displayBounds())
            try requireAccess()
            try perform(actions)
            try emit(["success": true, "focused_app": NSWorkspace.shared.frontmostApplication?.bundleIdentifier ?? ""])
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
