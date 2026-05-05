#include <stdio.h>

extern int lib_a_value(void);

int main(void) {
    printf("root app: lib_a=%d\n", lib_a_value());
    return 0;
}
