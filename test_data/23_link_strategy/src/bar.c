#include "helper.h"
#include "api.h"

int bar_api(int x) {
    return helper_compute(x) + 20;
}
