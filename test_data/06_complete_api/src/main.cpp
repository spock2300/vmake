#include "library.h"
#include <stdio.h>

int main() {
    printf("VMake Complete API Test\n");
    
    int result = library_compute(3, 4);
    printf("3 + 4 * 2 = %d\n", result);
    
    library_print_info();
    
    return 0;
}
