import Flutter
import UIKit

class SceneDelegate: FlutterSceneDelegate {
  override func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options connectionOptions: UIScene.ConnectionOptions) {
    if let response = connectionOptions.notificationResponse,
       let delegate = UIApplication.shared.delegate as? AppDelegate {
      _ = delegate.capturePush(response.notification.request.content.userInfo, opened: true)
    }
    super.scene(scene, willConnectTo: session, options: connectionOptions)
  }
}
