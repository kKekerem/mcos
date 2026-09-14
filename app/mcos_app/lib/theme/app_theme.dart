import 'package:flutter/material.dart';

import 'palette.dart';

/// Uygulamanın görsel dili.
///
/// ── Neden tek bir koyu tema ─────────────────────────────────────────────────
/// MCOS paneli koyu. Telefonda açık tema sunmak, iki ürünü birbirinden
/// ayırırdı ve kullanıcı çoğunlukla karanlık bir odada sunucusuna bakıyor.
/// Vurgu rengi ise panelden okunuyor (bkz. [build]).
class AppTheme {
  const AppTheme._();

  /// Köşe yarıçapı: panelin kart yarıçapıyla aynı his.
  static const radius = 12.0;
  static const radiusSmall = 8.0;

  static ThemeData build(Color accent) {
    final scheme = ColorScheme.dark(
      primary: accent,
      onPrimary: Palette.textOn,
      secondary: accent,
      onSecondary: Palette.textOn,
      surface: Palette.surface,
      onSurface: Palette.text,
      error: Palette.error,
      onError: Palette.text,
      outline: Palette.border,
    );

    return ThemeData(
      useMaterial3: true,
      brightness: Brightness.dark,
      colorScheme: scheme,

      // cardTheme ve dialogTheme BILEREK AYARLANMIYOR.
      //
      // Bu iki alanin bekledigi tip Flutter surumleri arasinda degisti
      // (CardTheme -> CardThemeData) ve ayarlamak, uygulamayi belirli bir
      // Flutter surumune baglardi. Kartlari zaten Container + BoxDecoration
      // ile ciziyoruz; AlertDialog de colorScheme.surface'i kullaniyor ki o
      // da Palette.surface.
      scaffoldBackgroundColor: Palette.bg,
      canvasColor: Palette.bg,
      dividerColor: Palette.divider,

      // Yazı tipi ailesi BELİRTİLMİYOR: Android'in kendi yazı tipi (Roboto)
      // her cihazda vardır ve Türkçe karakterleri doğru çizer. Özel bir
      // yazı tipi paketlemek, uygulamayı megabaytlarca büyütür ve bazı
      // karakterlerin eksik kalması riskini getirir.
      textTheme: const TextTheme(
        titleLarge: TextStyle(color: Palette.text, fontWeight: FontWeight.w600),
        titleMedium: TextStyle(color: Palette.text, fontWeight: FontWeight.w600),
        bodyLarge: TextStyle(color: Palette.text),
        bodyMedium: TextStyle(color: Palette.text),
        bodySmall: TextStyle(color: Palette.textDim),
        labelLarge: TextStyle(color: Palette.text, fontWeight: FontWeight.w600),
      ),

      appBarTheme: const AppBarTheme(
        backgroundColor: Palette.surface,
        foregroundColor: Palette.text,
        elevation: 0,
        centerTitle: false,
        titleTextStyle: TextStyle(
          color: Palette.text,
          fontSize: 18,
          fontWeight: FontWeight.w600,
        ),
      ),

      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: Palette.raised,
        hintStyle: const TextStyle(color: Palette.textFaint),
        labelStyle: const TextStyle(color: Palette.textDim),
        contentPadding:
            const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(radiusSmall),
          borderSide: const BorderSide(color: Palette.border),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(radiusSmall),
          borderSide: const BorderSide(color: Palette.border),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(radiusSmall),
          borderSide: BorderSide(color: accent, width: 2),
        ),
        errorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(radiusSmall),
          borderSide: const BorderSide(color: Palette.error),
        ),
      ),

      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: accent,
          foregroundColor: Palette.textOn,
          // 48: Material'ın önerdiği en küçük dokunma hedefi. Daha küçüğü
          // parmakla ıskalanır.
          minimumSize: const Size.fromHeight(48),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(radiusSmall),
          ),
          textStyle: const TextStyle(fontWeight: FontWeight.w600),
        ),
      ),

      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: Palette.text,
          side: const BorderSide(color: Palette.border),
          minimumSize: const Size.fromHeight(48),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(radiusSmall),
          ),
        ),
      ),

      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(foregroundColor: accent),
      ),

      switchTheme: SwitchThemeData(
        thumbColor: WidgetStateProperty.resolveWith(
          (s) => s.contains(WidgetState.selected) ? accent : Palette.textFaint,
        ),
        trackColor: WidgetStateProperty.resolveWith(
          (s) => s.contains(WidgetState.selected)
              ? accent.withValues(alpha: 0.35)
              : Palette.raised,
        ),
      ),

      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: Palette.surface,
        indicatorColor: accent.withValues(alpha: 0.18),
        labelTextStyle: WidgetStateProperty.all(
          const TextStyle(fontSize: 12, color: Palette.textDim),
        ),
        iconTheme: WidgetStateProperty.resolveWith(
          (s) => IconThemeData(
            color: s.contains(WidgetState.selected) ? accent : Palette.textDim,
          ),
        ),
      ),

      snackBarTheme: SnackBarThemeData(
        backgroundColor: Palette.raised,
        contentTextStyle: const TextStyle(color: Palette.text),
        behavior: SnackBarBehavior.floating,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(radiusSmall),
        ),
      ),

      progressIndicatorTheme: ProgressIndicatorThemeData(
        color: accent,
        linearTrackColor: Palette.raised,
      ),

      listTileTheme: const ListTileThemeData(
        textColor: Palette.text,
        iconColor: Palette.textDim,
      ),
    );
  }
}
