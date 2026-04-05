#include <stdio.h>

int main(int argc, char *argv[]) {
    printf("MyApp v1.0 - Firmware Test Application\n");
    if (argc > 1) {
        printf("args: ");
        for (int i = 1; i < argc; i++) {
            printf("%s ", argv[i]);
        }
        printf("\n");
    }
    return 0;
}
