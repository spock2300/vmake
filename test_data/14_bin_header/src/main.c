#include <stdio.h>

static const unsigned char hello[] = {
    #include "hello.h"
};
static const unsigned char empty[] = {
    #include "empty.h"
};

int main(void) {
    printf("hello: %zu bytes\n", sizeof(hello));
    printf("empty: %zu bytes\n", sizeof(empty));
    printf("data: ");
    for (size_t i = 0; i < sizeof(hello); i++) {
        putchar(hello[i]);
    }
    putchar('\n');
    return 0;
}
