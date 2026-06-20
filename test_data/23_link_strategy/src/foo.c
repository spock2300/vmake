#include "helper.h"
#include "api.h"

int foo_api(int x) {
    return helper_compute(x) + 10;
}
