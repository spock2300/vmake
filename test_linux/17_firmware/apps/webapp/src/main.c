#include "core.h"
#include "net.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <signal.h>
#include <sys/socket.h>
#include <netinet/in.h>

static volatile int running = 1;

static void on_signal(int sig) {
    (void)sig;
    running = 0;
}

int main(int argc, char *argv[]) {
    int port = NET_DEFAULT_PORT;
    if (argc > 1) {
        port = atoi(argv[1]);
    }

    signal(SIGINT, on_signal);
    signal(SIGTERM, on_signal);

    if (core_init() != 0) {
        fprintf(stderr, "core_init failed\n");
        return 1;
    }
    core_log("INFO", "webapp starting");

    int srv = net_listen_and_serve(port);
    if (srv < 0) {
        core_log("ERROR", net_last_error());
        return 2;
    }

    char response[256];
    snprintf(response, sizeof(response),
             "HTTP/1.0 200 OK\r\n"
             "Content-Type: text/plain\r\n"
             "\r\n"
             "webapp %s (port %d) ready\n"
             "core version: %d\n",
             core_build_id(), port, core_get_version());

    core_log("INFO", "accepting connections");
    while (running) {
        struct sockaddr_in client;
        (void)client;
        int c = accept(srv, NULL, NULL);
        if (c < 0) {
            if (!running) break;
            continue;
        }
        char buf[1024];
        net_recv(c, buf, sizeof(buf) - 1);
        net_send(c, response, (unsigned)strlen(response));
        net_close(c);
    }

    core_log("INFO", "webapp shutting down");
    net_close(srv);
    return 0;
}
