#include <stdio.h>
#include "subgen.h"

int get_magic_value(void) {
	return MAGIC_VALUE;
}

void print_info(void) {
	printf("sublib: magic=%d\n", MAGIC_VALUE);
}
