#include <stdio.h>
#include <string.h>

int main(void) {
    int errors = 0;

    if (CONFIG_FEATURE_FOO == 1) {
        printf("CONFIG_FEATURE_FOO=1 (ok)\n");
    } else {
        printf("ERROR: CONFIG_FEATURE_FOO=%d (expected 1)\n", CONFIG_FEATURE_FOO);
        errors++;
    }

    if (CONFIG_FEATURE_BAR == 0) {
        printf("CONFIG_FEATURE_BAR=0 (ok)\n");
    } else {
        printf("ERROR: CONFIG_FEATURE_BAR=%d (expected 0)\n", CONFIG_FEATURE_BAR);
        errors++;
    }

    if (CONFIG_BUFFER_SIZE == 4096) {
        printf("CONFIG_BUFFER_SIZE=4096 (ok)\n");
    } else {
        printf("ERROR: CONFIG_BUFFER_SIZE=%d (expected 4096)\n", CONFIG_BUFFER_SIZE);
        errors++;
    }

    if (strcmp(CONFIG_DEVICE_NAME, "uart0") == 0) {
        printf("CONFIG_DEVICE_NAME=uart0 (ok)\n");
    } else {
        printf("ERROR: CONFIG_DEVICE_NAME=%s (expected uart0)\n", CONFIG_DEVICE_NAME);
        errors++;
    }

    if (strcmp(CONFIG_PLATFORM, "linux") == 0) {
        printf("CONFIG_PLATFORM=linux (ok)\n");
    } else {
        printf("ERROR: CONFIG_PLATFORM=%s (expected linux)\n", CONFIG_PLATFORM);
        errors++;
    }

    if (CONFIG_PLATFORM_LINUX == 1) {
        printf("CONFIG_PLATFORM_LINUX=1 (ok)\n");
    } else {
        printf("ERROR: CONFIG_PLATFORM_LINUX=%d (expected 1)\n", CONFIG_PLATFORM_LINUX);
        errors++;
    }

    if (errors > 0) {
        printf("FAILED: %d errors\n", errors);
        return 1;
    }

    printf("All config defines tests passed!\n");
    return 0;
}
