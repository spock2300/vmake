#include <stdio.h>
#include "utils.h"

int main(void) {
    printf("Version: %s\n", get_version());
    printf("compute(3, 4) = %d\n", compute(3, 4));
    return 0;
}
