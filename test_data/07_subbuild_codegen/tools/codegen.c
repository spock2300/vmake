#include <stdio.h>
#include "tinyexpr.h"

#ifndef MAGIC
#define MAGIC 42
#endif

int main(int argc, char *argv[]) {
	if (argc < 2) {
		fprintf(stderr, "usage: codegen <output.h>\n");
		return 1;
	}

	double expr_result = te_interp("sqrt(2) + 1", 0);
	int magic_x2 = MAGIC * 2 + (int)expr_result;

	FILE *f = fopen(argv[1], "w");
	if (!f) {
		perror("fopen");
		return 1;
	}

	fprintf(f, "#ifndef GENERATED_H\n");
	fprintf(f, "#define GENERATED_H\n");
	fprintf(f, "#define GENERATED_MAGIC %d\n", magic_x2);
	fprintf(f, "#define TINYEXPR_RESULT %.6f\n", expr_result);
	fprintf(f, "#endif\n");

	fclose(f);
	return 0;
}
