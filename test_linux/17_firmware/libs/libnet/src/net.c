#include "net.h"
#include "core.h"

#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <string.h>
#include <errno.h>
#include <stdio.h>

static char last_error_buf[128] = "no error";

const char *net_last_error(void) {
    return last_error_buf;
}

static void set_err(const char *ctx) {
    snprintf(last_error_buf, sizeof(last_error_buf), "%s: %s", ctx, strerror(errno));
}

int net_init(int port) {
    (void)port;
    core_log("INFO", "libnet initialized");
    return 0;
}

int net_listen_and_serve(int port) {
    int s = socket(AF_INET, SOCK_STREAM, 0);
    if (s < 0) { set_err("socket"); return -1; }

    int yes = 1;
    setsockopt(s, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof(yes));

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_ANY);
    addr.sin_port = htons((unsigned short)port);

    if (bind(s, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
        set_err("bind");
        close(s);
        return -1;
    }
    if (listen(s, NET_MAX_CLIENTS) < 0) {
        set_err("listen");
        close(s);
        return -1;
    }

    char msg[64];
    snprintf(msg, sizeof(msg), "listening on port %d", port);
    core_log("INFO", msg);

    return s;
}

int net_send(int fd, const void *data, size_t len) {
    ssize_t n = send(fd, data, len, 0);
    if (n < 0) {
        set_err("send");
        return -1;
    }
    return (int)n;
}

int net_recv(int fd, void *buf, size_t len) {
    ssize_t n = recv(fd, buf, len, 0);
    if (n < 0) {
        set_err("recv");
        return -1;
    }
    return (int)n;
}

void net_close(int fd) {
    close(fd);
}
