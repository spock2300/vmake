#include <stdio.h>

int main(int argc, char *argv[]) {
	if (argc < 2) {
		fprintf(stderr, "usage: codegen <output.h>\n");
		return 1;
	}

	FILE *f = fopen(argv[1], "w");
	if (!f) {
		perror("fopen");
		return 1;
	}

	fprintf(f, "#ifndef GENERATED_H\n");
	fprintf(f, "#define GENERATED_H\n");
	fprintf(f, "#define GENERATED_VERSION \"1.0\"\n");
	fprintf(f, "#define GENERATED_MAGIC 42\n");
	fprintf(f, "#endif\n");

	fclose(f);
	return 0;
}
