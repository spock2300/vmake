#include <stdio.h>
#include <string.h>
#include "autoconf.h"

int main(void) {
    int errors = 0;

#ifdef CONFIG_FEATURE_FOO
    printf("CONFIG_FEATURE_FOO=1 (expected 1)\n");
#else
    printf("ERROR: CONFIG_FEATURE_FOO not defined (expected 1)\n");
    errors++;
#endif

#ifdef CONFIG_FEATURE_BAR
    printf("ERROR: CONFIG_FEATURE_BAR defined (expected undefined)\n");
    errors++;
#else
    printf("CONFIG_FEATURE_BAR undefined (ok)\n");
#endif

    printf("CONFIG_BUFFER_SIZE=%d (expected 4096)\n", CONFIG_BUFFER_SIZE);
    if (CONFIG_BUFFER_SIZE != 4096) errors++;

    printf("CONFIG_DEVICE_NAME=%s (expected uart0)\n", CONFIG_DEVICE_NAME);
    if (strcmp(CONFIG_DEVICE_NAME, "uart0") != 0) errors++;

    printf("CONFIG_PLATFORM=%s (expected linux)\n", CONFIG_PLATFORM);
    if (strcmp(CONFIG_PLATFORM, "linux") != 0) errors++;

#ifdef CONFIG_PLATFORM_LINUX
    printf("CONFIG_PLATFORM_LINUX=1 (ok)\n");
#else
    printf("ERROR: CONFIG_PLATFORM_LINUX not defined\n");
    errors++;
#endif

#ifdef CONFIG_PLATFORM_MACOS
    printf("ERROR: CONFIG_PLATFORM_MACOS defined\n");
    errors++;
#endif

    if (errors > 0) {
        printf("FAILED: %d errors\n", errors);
        return 1;
    }

    printf("All config header tests passed!\n");
    return 0;
}
