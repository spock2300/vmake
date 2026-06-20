#include <stdio.h>
#include "foo.h"

int main(void) {
    foo_init();
    int v = foo_api(21);
    printf("foo_api(21) = %d\n", v);
    return 0;
}
