#include <stdio.h>
#include <zlib.h>

int main(void) {
    printf("zlib version: %s\n", zlibVersion());
    return 0;
}
