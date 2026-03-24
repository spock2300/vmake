#include <stdio.h>
#include <math.h>
#include "tinyexpr.h"

int main() {
    double result;

    result = te_interp("sin(3.14159/4) + sqrt(2)", 0);
    printf("sin(pi/4) + sqrt(2) = %.6f\n", result);

    result = te_interp("2^10 + log(10)", 0);
    printf("2^10 + log(10) = %.6f\n", result);

    result = te_interp("abs(-42) * 3", 0);
    printf("abs(-42) * 3 = %.6f\n", result);

    return 0;
}
