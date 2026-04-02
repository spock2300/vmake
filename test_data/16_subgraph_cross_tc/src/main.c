#include <stdio.h>
#include "crosslib.h"

int main(void) {
	int v = get_value();
	printf("cross_tc: value=%d\n", v);
	return v == 77 ? 0 : 1;
}
