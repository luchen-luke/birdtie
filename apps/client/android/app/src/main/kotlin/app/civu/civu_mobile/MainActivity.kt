package app.civu.civu_mobile

import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import android.content.Intent
import android.app.NotificationChannel
import android.app.NotificationManager
import android.os.Build

class MainActivity : FlutterActivity() {
    private var localText: LocalImageTextRecognizer? = null
    private var voiceInput: VoiceInputRecognizer? = null
    private var friendAudio: FriendAudio? = null
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val manager = getSystemService(NotificationManager::class.java)
            manager.createNotificationChannel(NotificationChannel(
                "birdtie_updates", "Birdtie 通知", NotificationManager.IMPORTANCE_DEFAULT
            ))
        }
        localText?.close()
        localText = LocalImageTextRecognizer(applicationContext, flutterEngine.dartExecutor.binaryMessenger)
        voiceInput?.close()
        voiceInput = VoiceInputRecognizer(this, flutterEngine.dartExecutor.binaryMessenger)
        voiceInput?.setQaTiming(intent?.getBooleanExtra("birdtie.qa.voiceTiming", false) == true)
        friendAudio?.close()
        friendAudio = FriendAudio(this, flutterEngine.dartExecutor.binaryMessenger)
    }
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        voiceInput?.setQaTiming(intent.getBooleanExtra("birdtie.qa.voiceTiming", false))
    }
    override fun onResume() {
        super.onResume()
        voiceInput?.onResume()
        friendAudio?.onResume()
    }
    override fun onPause() {
        voiceInput?.onPause()
        friendAudio?.onPause()
        super.onPause()
    }
    override fun onStop() {
        voiceInput?.onStop()
        friendAudio?.onStop()
        super.onStop()
    }
    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        voiceInput?.onPermissionsResult(requestCode, permissions, grantResults)
        friendAudio?.onPermissionsResult(requestCode, permissions, grantResults)
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
    }
    @Deprecated("Activity result forwarding for the owned speech child")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        voiceInput?.onActivityResult(requestCode, resultCode, data)
        super.onActivityResult(requestCode, resultCode, data)
    }
    override fun cleanUpFlutterEngine(flutterEngine: FlutterEngine) {
        voiceInput?.close()
        voiceInput = null
        friendAudio?.close()
        friendAudio = null
        localText?.close()
        localText = null
        super.cleanUpFlutterEngine(flutterEngine)
    }
}
