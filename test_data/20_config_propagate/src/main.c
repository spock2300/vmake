#include <stdio.h>
#include "chip.h"

#ifdef CONFIG_CHIP_FEATURE_A
#define HAS_FEATURE_A 1
#else
#define HAS_FEATURE_A 0
#endif

#ifdef CONFIG_APP_VERBOSE
#define VERBOSE 1
#else
#define VERBOSE 0
#endif

int main(void) {
    printf("feature_a=%d mode=%d verbose=%d\n",
           HAS_FEATURE_A, chip_speed(), VERBOSE);
    printf("chip_mode=%d\n", chip_get_mode());
    return 0;
}
