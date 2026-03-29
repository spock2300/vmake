#include <stdio.h>
#include <mathlib.h>
#include <greeter.h>

int main() {
    printf("%s\n", greet("VMake"));
    printf("mathlib_add(2, 3) = %d\n", mathlib_add(2, 3));
    printf("mathlib_multiply(4, 5) = %d\n", mathlib_multiply(4, 5));
    return 0;
}
