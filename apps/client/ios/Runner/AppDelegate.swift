import Flutter
import UIKit
import UserNotifications

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  private var pushSink: FlutterEventSink?
  private var pushToken: String?
  private var initialPush: [String: String]?
  private var pendingRegistration: FlutterResult?
  private var pendingRegistrationID = UUID()
  private var pushEvents: BirdtiePushEvents?
  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    UNUserNotificationCenter.current().delegate = self
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
    let messenger = engineBridge.applicationRegistrar.messenger()
    let channel = FlutterMethodChannel(name: "birdtie/system-push", binaryMessenger: messenger)
    channel.setMethodCallHandler { [weak self] call, result in
      guard let self = self else { result(FlutterError(code: "UNAVAILABLE", message: nil, details: nil)); return }
      switch call.method {
      case "initial": let event = self.initialPush; self.initialPush = nil; result(event)
      case "cancelRegistration":
        self.pendingRegistration?(FlutterError(code: "CANCELLED", message: nil, details: nil))
        self.pendingRegistration = nil; self.pendingRegistrationID = UUID(); result(nil)
      case "unregister":
        self.pendingRegistration?(FlutterError(code: "CANCELLED", message: nil, details: nil))
        self.pendingRegistration = nil; self.pendingRegistrationID = UUID(); self.pushToken = nil
        UIApplication.shared.unregisterForRemoteNotifications(); result(nil)
      case "register": self.registerPush(requestPermission: true, result: result)
      case "status": self.registerPush(requestPermission: false, result: result)
      default: result(FlutterMethodNotImplemented)
      }
    }
    let events = BirdtiePushEvents(delegate: self)
    pushEvents = events
    FlutterEventChannel(name: "birdtie/system-push/events", binaryMessenger: messenger).setStreamHandler(events)
  }

  private func registerPush(requestPermission: Bool, result: @escaping FlutterResult) {
    guard pendingRegistration == nil else { result(FlutterError(code: "BUSY", message: nil, details: nil)); return }
    let generation = UUID(); pendingRegistrationID = generation; pendingRegistration = result
    let center = UNUserNotificationCenter.current()
    func inspect() {
      center.getNotificationSettings { [weak self] settings in
        DispatchQueue.main.async {
          guard let self = self, self.pendingRegistrationID == generation else { return }
          guard settings.authorizationStatus == .authorized || settings.authorizationStatus == .provisional else {
            self.pendingRegistration = nil; result(["permission": "DENIED"]); return
          }
          UIApplication.shared.registerForRemoteNotifications()
        }
      }
    }
    if requestPermission {
      center.requestAuthorization(options: [.alert, .sound]) { _, _ in inspect() }
    } else { inspect() }
    DispatchQueue.main.asyncAfter(deadline: .now() + 20) { [weak self] in
      guard let self = self, self.pendingRegistrationID == generation, self.pendingRegistration != nil else { return }
      self.pendingRegistration = nil
      result(FlutterError(code: "UNAVAILABLE", message: nil, details: nil))
    }
  }

  override func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
    let previousToken = pushToken
    pushToken = deviceToken.map { String(format: "%02x", $0) }.joined()
    if let previousToken = previousToken, let token = pushToken, previousToken != token {
      pushSink?(["type": "ADDRESS", "address": token])
    }
    let result = pendingRegistration; pendingRegistration = nil
    let generation = pendingRegistrationID
    if let result = result {
      UNUserNotificationCenter.current().getNotificationSettings { [weak self] settings in
        DispatchQueue.main.async {
          guard let self = self, self.pendingRegistrationID == generation, let token = self.pushToken else { result(FlutterError(code: "CANCELLED", message: nil, details: nil)); return }
          guard settings.authorizationStatus == .authorized || settings.authorizationStatus == .provisional else { result(["permission": "DENIED"]); return }
          result(["address": token, "permission": settings.authorizationStatus == .provisional ? "PROVISIONAL" : "AUTHORIZED"])
        }
      }
    }
    super.application(application, didRegisterForRemoteNotificationsWithDeviceToken: deviceToken)
  }

  override func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
    let result = pendingRegistration; pendingRegistration = nil
    result?(FlutterError(code: "UNAVAILABLE", message: nil, details: nil))
    super.application(application, didFailToRegisterForRemoteNotificationsWithError: error)
  }

  func capturePush(_ info: [AnyHashable: Any], opened: Bool) -> Bool {
    let keys = ["schema", "eventId", "bindingId", "bindingVersion"]
    // aps is system presentation only; every Birdtie data key is closed.
    guard info.keys.allSatisfy({ $0 as? String == "aps" || keys.contains($0 as? String ?? "") }) else { return false }
    var event: [String: String] = [:]
    for key in keys { guard let value = info[key] as? String else { return false }; event[key] = value }
    guard event["schema"] == "birdtie-push-v1", UUID(uuidString: event["eventId"]!) != nil,
          UUID(uuidString: event["bindingId"]!) != nil,
          let version = Int64(event["bindingVersion"]!), version > 0,
          String(version) == event["bindingVersion"]! else { return false }
    if opened && pushSink == nil { initialPush = event }
    else { var payload: [String: Any] = event; payload["opened"] = opened; pushSink?(payload) }
    return true
  }
  override func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
    withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
    if capturePush(notification.request.content.userInfo, opened: false) { completionHandler([]) }
    else { super.userNotificationCenter(center, willPresent: notification, withCompletionHandler: completionHandler) }
  }
  override func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
    withCompletionHandler completionHandler: @escaping () -> Void) {
    if capturePush(response.notification.request.content.userInfo, opened: true) { completionHandler() }
    else { super.userNotificationCenter(center, didReceive: response, withCompletionHandler: completionHandler) }
  }
  fileprivate func setPushSink(_ sink: FlutterEventSink?) { pushSink = sink }
}

private class BirdtiePushEvents: NSObject, FlutterStreamHandler {
  weak var delegate: AppDelegate?
  init(delegate: AppDelegate) { self.delegate = delegate }
  func onListen(withArguments arguments: Any?, eventSink events: @escaping FlutterEventSink) -> FlutterError? { delegate?.setPushSink(events); return nil }
  func onCancel(withArguments arguments: Any?) -> FlutterError? { delegate?.setPushSink(nil); return nil }
}
