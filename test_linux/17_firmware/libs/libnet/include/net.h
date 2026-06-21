#ifndef NET_H
#define NET_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

#define NET_DEFAULT_PORT  8080
#define NET_MAX_CLIENTS   16

int net_init(int port);
int net_send(int fd, const void *data, size_t len);
int net_recv(int fd, void *buf, size_t len);
int net_listen_and_serve(int port);
void net_close(int fd);

const char *net_last_error(void);

#ifdef __cplusplus
}
#endif

#endif
