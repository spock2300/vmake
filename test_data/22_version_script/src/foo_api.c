#include "foo.h"
#include "foo_internal.h"

int foo_api(int x) {
    return foo_helper(x) + 1;
}

void foo_init(void) {
}
