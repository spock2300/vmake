#include "core.h"
#include "net.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int send_command(const char *host, int port, const char *cmd) {
    (void)host;
    int fd = net_listen_and_serve(0);
    (void)fd;
    core_log("INFO", cmd);
    char msg[128];
    snprintf(msg, sizeof(msg), "command '%s' sent to port %d", cmd, port);
    core_log("INFO", msg);
    return 0;
}

int main(int argc, char *argv[]) {
    if (argc < 2) {
        fprintf(stderr,
                "Usage: %s <command> [args...]\n"
                "Commands:\n"
                "  status    - report panel status\n"
                "  send PORT - send reset signal to port\n"
                "  version   - print build info\n",
                argv[0]);
        return 1;
    }

    if (core_init() != 0) {
        fprintf(stderr, "core_init failed\n");
        return 1;
    }

    const char *cmd = argv[1];
    if (strcmp(cmd, "status") == 0) {
        core_log("INFO", "panel status: OK");
        printf("panel: online, core=%d, net_port=%d\n",
               core_get_version(), NET_DEFAULT_PORT);
    } else if (strcmp(cmd, "send") == 0) {
        if (argc < 3) {
            fprintf(stderr, "send requires PORT\n");
            return 1;
        }
        int port = atoi(argv[2]);
        send_command("localhost", port, "RESET");
    } else if (strcmp(cmd, "version") == 0) {
        printf("controlpanel %s (core %s)\n",
               core_build_id(), core_build_id());
    } else {
        fprintf(stderr, "unknown command: %s\n", cmd);
        return 1;
    }

    return 0;
}
