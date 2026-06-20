#include "foo_internal.h"

int foo_helper(int x) {
    return x * 2;
}

int foo_internal_state = 42;
