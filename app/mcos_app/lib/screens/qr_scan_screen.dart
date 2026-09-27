import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../services/pair_uri.dart';
import '../theme/palette.dart';
import '../services/panel_text.dart';

/// MCOS panelindeki eşleşme QR'ını okuyan ekran.
///
/// Başarılı okumada çözülmüş [PairInfo] ile kapanır; kullanıcı vazgeçerse
/// null döner.
///
/// ── Neden yalnızca mcos:// kabul ediliyor ───────────────────────────────────
/// Kamera ortamdaki her kodu yakalar (bir ürün barkodu, bir web adresi).
/// Her birini "hata" diye göstermek, kullanıcıyı asıl QR'ı okutmaktan
/// alıkoyar. mcos:// ile başlamayan kodlar için yalnızca alttaki ipucu
/// satırı güncelleniyor; mcos:// ile başlayıp BOZUK olan kod ise açıkça
/// söyleniyor (yanlış sürüm, eksik jeton…).
class QrScanScreen extends StatefulWidget {
  const QrScanScreen({super.key});

  @override
  State<QrScanScreen> createState() => _QrScanScreenState();
}

class _QrScanScreenState extends State<QrScanScreen> {
  final _controller = MobileScannerController(
    // Yalnızca QR: diğer biçimleri aramamak, okumayı hızlandırır ve yanlış
    // pozitifleri (ör. bir barkodun parçası) ortadan kaldırır.
    formats: const [BarcodeFormat.qrCode],
    detectionSpeed: DetectionSpeed.noDuplicates,
  );

  bool _done = false;
  String? _hint;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _onDetect(BarcodeCapture capture) {
    if (_done) return;
    for (final b in capture.barcodes) {
      final raw = b.rawValue;
      if (raw == null || raw.isEmpty) continue;
      if (!looksLikePairUri(raw)) {
        setState(() => _hint =
            'Bu bir MCOS kodu değil. Panelde: $panelQrPath.',);
        continue;
      }
      try {
        final info = parsePairUri(raw);
        _done = true;
        Navigator.of(context).pop(info);
        return;
      } on PairFormatException catch (e) {
        setState(() => _hint = e.message);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        title: const Text('QR ile bağlan'),
        actions: [
          IconButton(
            tooltip: 'Fener',
            icon: const Icon(Icons.flashlight_on_outlined),
            onPressed: () => _controller.toggleTorch(),
          ),
        ],
      ),
      body: Stack(
        fit: StackFit.expand,
        children: [
          MobileScanner(
            controller: _controller,
            onDetect: _onDetect,
            errorBuilder: (context, error) => _ScanError(error: error),
          ),
          // Hedef çerçevesi: kullanıcı kodu nereye tutacağını görsün.
          IgnorePointer(
            child: Center(
              child: Container(
                width: 250,
                height: 250,
                decoration: BoxDecoration(
                  border: Border.all(color: Palette.accent, width: 3),
                  borderRadius: BorderRadius.circular(16),
                ),
              ),
            ),
          ),
          Positioned(
            left: 16,
            right: 16,
            bottom: 32,
            child: Container(
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(
                color: Colors.black.withValues(alpha: 0.7),
                borderRadius: BorderRadius.circular(10),
              ),
              child: Text(
                _hint ??
                    'MCOS panelinde: $panelQrPath.\n'
                        'Ekrandaki kareyi çerçevenin içine alın.',
                textAlign: TextAlign.center,
                style: const TextStyle(color: Colors.white, fontSize: 14),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Kamera açılamadığında gösterilen açıklama.
///
/// mobile_scanner hatayı İngilizce bir kodla bildiriyor
/// (permissionDenied, unsupported…). Kullanıcı bunu değil, ne yapacağını
/// görmeli; elle giriş her zaman geri dönüş yolu olarak duruyor.
class _ScanError extends StatelessWidget {
  const _ScanError({required this.error});

  final MobileScannerException error;

  @override
  Widget build(BuildContext context) {
    final text = switch (error.errorCode) {
      MobileScannerErrorCode.permissionDenied =>
        'Kamera izni verilmedi.\n\nQR okumak için Ayarlar → Uygulamalar → '
            'MCOS → İzinler bölümünden kameraya izin verin. Ya da geri dönüp '
            'adresi ve jetonu elle girin.',
      MobileScannerErrorCode.unsupported =>
        'Bu cihazda kamera kullanılamıyor. Geri dönüp adresi ve jetonu elle '
            'girin.',
      _ =>
        'Kamera açılamadı. Başka bir uygulama kamerayı kullanıyor olabilir; '
            'kapatıp yeniden deneyin ya da geri dönüp elle girin.',
    };
    return ColoredBox(
      color: Palette.bg,
      child: Center(
        child: Padding(
          padding: const EdgeInsets.all(28),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.no_photography_outlined,
                  size: 48, color: Palette.textDim,),
              const SizedBox(height: 16),
              Text(
                text,
                textAlign: TextAlign.center,
                style: const TextStyle(color: Palette.text, fontSize: 14),
              ),
              const SizedBox(height: 20),
              OutlinedButton(
                onPressed: () => Navigator.of(context).pop(),
                child: const Text('Elle gir'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
