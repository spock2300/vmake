#include <stdio.h>
#include "mylib.h"

int main() {
    if (add(1, 1) == 2 && multiply(2, 3) == 6) {
        printf("All tests passed!\n");
        return 0;
    }
    printf("Tests failed!\n");
    return 1;
}
