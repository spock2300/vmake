#include "core.h"
#include <stdio.h>
#include <time.h>
#include <string.h>
#include <unistd.h>

static int log_fd = 2;

int core_init(void) {
    return core_log("INFO", "libcore initialized");
}

int core_get_version(void) {
    return CORE_VERSION;
}

const char *core_build_id(void) {
    return CORE_BUILD_ID;
}

int core_log(const char *level, const char *msg) {
    time_t now = time(NULL);
    struct tm tm;
    localtime_r(&now, &tm);
    char timebuf[32];
    strftime(timebuf, sizeof(timebuf), "%Y-%m-%d %H:%M:%S", &tm);
    char line[256];
    int n = snprintf(line, sizeof(line), "[%s] %s core: %s\n", timebuf, level, msg);
    if (n <= 0) return n;
    return write(log_fd, line, (size_t)n);
}

int core_log_set_fd(int fd) {
    int old = log_fd;
    log_fd = fd;
    return old;
}

unsigned long core_hash(const void *data, int len) {
    const unsigned char *p = (const unsigned char *)data;
    unsigned long h = 5381;
    for (int i = 0; i < len; i++) {
        h = ((h << 5) + h) + p[i];
    }
    return h;
}
