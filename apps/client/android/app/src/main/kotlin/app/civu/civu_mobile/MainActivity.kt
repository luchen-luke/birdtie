package app.civu.civu_mobile

import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine

class MainActivity : FlutterActivity() {
    private var localText: LocalImageTextRecognizer? = null
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        localText?.close()
        localText = LocalImageTextRecognizer(applicationContext, flutterEngine.dartExecutor.binaryMessenger)
    }
    override fun cleanUpFlutterEngine(flutterEngine: FlutterEngine) {
        localText?.close()
        localText = null
        super.cleanUpFlutterEngine(flutterEngine)
    }
}
