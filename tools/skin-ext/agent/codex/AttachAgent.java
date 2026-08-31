package codex;

import com.sun.tools.attach.VirtualMachine;

public final class AttachAgent {
    private AttachAgent() {}

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            throw new IllegalArgumentException("uso: AttachAgent <pid> <agent.jar>");
        }
        VirtualMachine vm = VirtualMachine.attach(args[0]);
        try {
            vm.loadAgent(args[1]);
        } finally {
            vm.detach();
        }
    }
}
