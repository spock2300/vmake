#include <stdio.h>
#include "output/generated.h"

int main(void) {
	printf("magic=%d, tinyexpr=%.6f\n", GENERATED_MAGIC, TINYEXPR_RESULT);
	return 0;
}
