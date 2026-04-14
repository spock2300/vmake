#include <stdio.h>

#ifndef MAGIC
#define MAGIC 42
#endif

int main(int argc, char *argv[]) {
	if (argc < 2) {
		fprintf(stderr, "usage: gen <output.h>\n");
		return 1;
	}

	FILE *f = fopen(argv[1], "w");
	if (!f) {
		perror("fopen");
		return 1;
	}

	fprintf(f, "#ifndef SUBGEN_H\n");
	fprintf(f, "#define SUBGEN_H\n");
	fprintf(f, "#define MAGIC_VALUE %d\n", MAGIC * 3);
	fprintf(f, "int get_magic_value(void);\n");
	fprintf(f, "void print_info(void);\n");
	fprintf(f, "#endif\n");

	fclose(f);
	return 0;
}
