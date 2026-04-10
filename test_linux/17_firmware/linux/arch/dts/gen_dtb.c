#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "autoconf.h"

#ifndef CONFIG_ARCH
#define CONFIG_ARCH "unknown"
#endif

int main(int argc, char *argv[])
{
	if (argc < 2) {
		fprintf(stderr, "Usage: gen_dtb <output.dtb>\n");
		return 1;
	}

	FILE *f = fopen(argv[1], "wb");
	if (!f) {
		perror("fopen");
		return 1;
	}

	uint8_t magic[] = {0xd0, 0x0d, 0xfe, 0xed};
	fwrite(magic, 1, 4, f);

	const char *compat = "vmake,soc-" CONFIG_ARCH;
	uint32_t len = (uint32_t)strlen(compat) + 1;
	fwrite(&len, 4, 1, f);
	fwrite(compat, 1, len, f);

	const char *model = "VMake Board Simulator";
	len = (uint32_t)strlen(model) + 1;
	fwrite(&len, 4, 1, f);
	fwrite(model, 1, len, f);

	uint32_t flags = 0;
#ifdef CONFIG_NET
	flags |= (1 << 0);
#endif
#ifdef CONFIG_BLK_DEV
	flags |= (1 << 1);
#endif
#ifdef CONFIG_I2C
	flags |= (1 << 2);
#endif
#ifdef CONFIG_SERIAL
	flags |= (1 << 3);
#endif
#ifdef CONFIG_GPIO
	flags |= (1 << 4);
#endif
	fwrite(&flags, 4, 1, f);

	fclose(f);
	return 0;
}
