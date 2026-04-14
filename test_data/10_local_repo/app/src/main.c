#include <stdio.h>
#include "mylib.h"

int main(void) {
    int result = mylib_add(2, 3);
    printf("2 + 3 = %d\n", result);
    return 0;
}
