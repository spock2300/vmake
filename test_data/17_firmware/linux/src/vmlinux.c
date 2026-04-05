#include <stdio.h>
#include "autoconf.h"

#ifndef CONFIG_LOCALVERSION
#define CONFIG_LOCALVERSION ""
#endif
#ifndef CONFIG_ARCH
#define CONFIG_ARCH "unknown"
#endif

int main(void)
{
	printf("Linux version 6.6.0" CONFIG_LOCALVERSION " (vmake simulator)\n");
	printf("  ARCH:     %s\n", CONFIG_ARCH);
	printf("  NET:      %s\n", CONFIG_NET ? "y" : "n");
	printf("  BLK_DEV:  %s\n", CONFIG_BLK_DEV ? "y" : "n");
	printf("  I2C:      %s\n", CONFIG_I2C ? "y" : "n");
	printf("  SPI:      %s\n", CONFIG_SPI ? "y" : "n");
	printf("  SERIAL:   %s\n", CONFIG_SERIAL ? "y" : "n");
	printf("  GPIO:     %s\n", CONFIG_GPIO ? "y" : "n");
	printf("  EXT4:     %s\n", CONFIG_EXT4 ? "y" : "n");
	printf("  SQUASHFS: %s\n", CONFIG_SQUASHFS ? "y" : "n");
	printf("  OVERLAY:  %s\n", CONFIG_OVERLAY_FS ? "y" : "n");
	return 0;
}
