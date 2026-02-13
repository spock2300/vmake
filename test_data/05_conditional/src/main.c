#include <stdio.h>

int main() {
#ifdef FEATURE_A
    printf("Feature A enabled\n");
#endif
#ifdef FEATURE_B
    printf("Feature B enabled\n");
#endif
#ifdef DEBUG_MODE
    printf("Debug mode\n");
#endif
#ifdef VERBOSE
    printf("Verbose output\n");
#endif
    printf("Platform: %s\n", PLATFORM);
    return 0;
}
