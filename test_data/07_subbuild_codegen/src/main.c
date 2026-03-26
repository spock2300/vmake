#include <stdio.h>
#include "output/generated.h"

int main(void) {
	printf("version=%s magic=%d\n", GENERATED_VERSION, GENERATED_MAGIC);
	return 0;
}
