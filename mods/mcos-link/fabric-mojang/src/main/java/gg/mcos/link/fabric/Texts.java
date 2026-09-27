package gg.mcos.link.fabric;

import gg.mcos.link.Msg;
import net.minecraft.ChatFormatting;
import net.minecraft.network.chat.Component;
import net.minecraft.network.chat.MutableComponent;

/** {@link Msg} → Minecraft metni (26.x'in resmî adları). */
final class Texts {

    private Texts() {
    }

    static Component of(Msg msg) {
        MutableComponent out = Component.empty();
        for (Msg.Part p : msg.parts()) {
            out.append(Component.literal(p.text()).withStyle(color(p.color())));
        }
        return out;
    }

    private static ChatFormatting color(Msg.Color c) {
        return switch (c) {
            case WHITE -> ChatFormatting.WHITE;
            case GRAY -> ChatFormatting.GRAY;
            case DARK_GRAY -> ChatFormatting.DARK_GRAY;
            case AQUA -> ChatFormatting.AQUA;
            case DARK_AQUA -> ChatFormatting.DARK_AQUA;
            case GREEN -> ChatFormatting.GREEN;
            case RED -> ChatFormatting.RED;
        };
    }
}
