#include "library.h"
#include <stdio.h>

#ifdef USE_SSL
#include <openssl/ssl.h>
#endif

int library_compute(int x, int y) {
    int sum = utils_add(x, y);
    return core_process(sum);
}

void library_print_info() {
#ifdef USE_SSL
    printf("SSL enabled, version: %s\n", SSL_VERSION);
#else
    printf("SSL disabled\n");
#endif
#ifdef DEBUG
    printf("Debug mode active\n");
#endif
#ifdef VERBOSE_MODE
    printf("Verbose mode active\n");
#endif
    printf("Thread count: %d\n", THREAD_COUNT);
    printf("Install prefix: %s\n", PREFIX);
}
