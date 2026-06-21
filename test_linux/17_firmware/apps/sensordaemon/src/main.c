#include "core.h"

#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <signal.h>
#include <time.h>

static volatile int running = 1;

static void on_signal(int sig) {
    (void)sig;
    running = 0;
}

static int read_sensor(int idx) {
    return (idx * 13 + (int)(time(NULL) & 0xffff)) & 0x3ff;
}

int main(int argc, char *argv[]) {
    int interval_us = 1000000;
    if (argc > 1) {
        interval_us = atoi(argv[1]) * 1000;
    }

    signal(SIGINT, on_signal);
    signal(SIGTERM, on_signal);

    if (core_init() != 0) {
        fprintf(stderr, "core_init failed\n");
        return 1;
    }
    core_log("INFO", "sensordaemon starting");

    int sensor_count = 4;
    int tick = 0;
    while (running) {
        char msg[128];
        unsigned long agg = 0;
        for (int i = 0; i < sensor_count; i++) {
            int v = read_sensor(i);
            agg += (unsigned long)v;
        }
        agg = core_hash(&agg, sizeof(agg));
        snprintf(msg, sizeof(msg), "tick=%d sensors=%d aggregate_hash=0x%lx",
                 tick++, sensor_count, agg);
        core_log("DEBUG", msg);
        usleep((useconds_t)interval_us);
    }

    core_log("INFO", "sensordaemon shutting down");
    return 0;
}
