#include "core.h"
#include <stdio.h>

static int initialized = 0;

void core_init() {
    initialized = 1;
    printf("Core initialized\n");
}

int core_process(int value) {
    if (!initialized) {
        core_init();
    }
    return value * 2;
}
