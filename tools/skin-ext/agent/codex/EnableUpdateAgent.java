package codex;

import gearth.app.protocol.connection.packetsafety.PacketSafetyManager;
import gearth.app.protocol.connection.packetsafety.SafePacketsContainer;
import gearth.protocol.HMessage;

import java.lang.instrument.Instrumentation;
import java.lang.reflect.Field;
import java.util.Map;

/** Libera o UPDATE nas instâncias headless já em execução. */
public final class EnableUpdateAgent {
    private EnableUpdateAgent() {}

    @SuppressWarnings("unchecked")
    public static void agentmain(String ignored, Instrumentation instrumentation) throws Exception {
        PacketSafetyManager manager = PacketSafetyManager.PACKET_SAFETY_MANAGER;
        Field field = PacketSafetyManager.class.getDeclaredField("safePacketContainers");
        field.setAccessible(true);
        Map<String, SafePacketsContainer> containers =
                (Map<String, SafePacketsContainer>) field.get(manager);
        for (SafePacketsContainer container : containers.values()) {
            container.validateSafePacket(44, HMessage.Direction.TOSERVER);
        }
        System.out.printf("CODEX_UPDATE_PACKET_ENABLED containers=%d%n", containers.size());
        System.out.flush();
    }
}
