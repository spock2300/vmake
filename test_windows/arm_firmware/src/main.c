#include "value.h"

extern int support(void);
volatile int result;

void _start(void) {
    result = support() + VALUE;
    for (;;) {}
}
