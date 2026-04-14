#include <stdio.h>

int main(void) {
#ifdef USE_SSL
    printf("SSL Enabled\n");
#endif
#ifdef DEBUG
    printf("Debug Mode\n");
#endif
    printf("Optimization: %s\n", OPT_LEVEL);
    return 0;
}
