package gg.mcos.link.fabric;

import gg.mcos.link.Msg;
import net.minecraft.text.MutableText;
import net.minecraft.text.Text;
import net.minecraft.util.Formatting;

/** {@link Msg} → Minecraft metni (Yarn adları). */
final class Texts {

    private Texts() {
    }

    static Text of(Msg msg) {
        MutableText out = Text.empty();
        for (Msg.Part p : msg.parts()) {
            out.append(Text.literal(p.text()).formatted(color(p.color())));
        }
        return out;
    }

    private static Formatting color(Msg.Color c) {
        return switch (c) {
            case WHITE -> Formatting.WHITE;
            case GRAY -> Formatting.GRAY;
            case DARK_GRAY -> Formatting.DARK_GRAY;
            case AQUA -> Formatting.AQUA;
            case DARK_AQUA -> Formatting.DARK_AQUA;
            case GREEN -> Formatting.GREEN;
            case RED -> Formatting.RED;
        };
    }
}
