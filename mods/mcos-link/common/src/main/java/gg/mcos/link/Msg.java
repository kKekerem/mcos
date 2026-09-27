package gg.mcos.link;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

/**
 * Sürümden bağımsız, renkli bir metin satırı.
 *
 * <p><b>Neden kendi türümüz?</b> Minecraft'ın metin sınıfının adı eşlemeye
 * göre değişir: Yarn'da ({@code 1.20.5}–{@code 1.21.11}) {@code Text} ve
 * {@code Formatting}, 26.x'in resmî adlarında {@code Component} ve
 * {@code ChatFormatting}. Aktarım, sohbet ve komut mantığı bu sınıfla
 * konuşur; oyunun metnine çeviri yalnızca sürüm bağdaştırıcısında yapılır.
 * Böylece iş mantığı her sürüm için ayrı ayrı kopyalanmaz — kopyalanan
 * mantık er ya da geç birinde düzeltilip ötekinde unutulur.
 *
 * <p>Her parça KENDİ rengini taşır; bağdaştırıcı parçaları art arda
 * ekler. Minecraft'ta alt metin rengini üstünden devralır, ama her parçaya
 * açıkça renk verildiği için sonuç eski iç içe {@code append} zinciriyle
 * birebir aynıdır.
 */
public final class Msg {

    /**
     * Kullandığımız renkler.
     *
     * <p>Adlar kasıtlı olarak Minecraft'ın renk sabitleriyle AYNI; ama
     * bağdaştırıcılar {@code valueOf} ile değil açık bir {@code switch} ile
     * çevirir. Böylece bir sürümde sabitin adı değişirse bu, çalışma anında
     * değil derlemede ortaya çıkar.
     */
    public enum Color {
        WHITE, GRAY, DARK_GRAY, AQUA, DARK_AQUA, GREEN, RED
    }

    /** Tek renkli bir metin parçası. */
    public record Part(String text, Color color) {
    }

    private final List<Part> parts = new ArrayList<>();

    private Msg() {
    }

    /** Tek parçalı bir metin. */
    public static Msg of(String text, Color color) {
        return new Msg().then(text, color);
    }

    /** Sona bir parça ekler (zincirleme için {@code this} döner). */
    public Msg then(String text, Color color) {
        parts.add(new Part(text, color));
        return this;
    }

    public List<Part> parts() {
        return Collections.unmodifiableList(parts);
    }

    /** Renksiz düz metin (günlük ve hata ayıklama için). */
    @Override
    public String toString() {
        StringBuilder b = new StringBuilder();
        for (Part p : parts) {
            b.append(p.text());
        }
        return b.toString();
    }
}
