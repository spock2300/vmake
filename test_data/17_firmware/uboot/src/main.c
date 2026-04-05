#include <stdio.h>
#include "autoconf.h"

#ifndef CONFIG_VERSION
#define CONFIG_VERSION "unknown"
#endif
#ifndef CONFIG_ENV_SIZE
#define CONFIG_ENV_SIZE 0
#endif
#ifndef CONFIG_BOARD_NAME
#define CONFIG_BOARD_NAME "unknown"
#endif

int main(void)
{
	printf("U-Boot " CONFIG_VERSION " (vmake simulator)\n");
	printf("  Board:    %s\n", CONFIG_BOARD_NAME);
	printf("  FIT:      %s\n", CONFIG_FIT ? "y" : "n");
	printf("  ENV_SIZE: 0x%x\n", CONFIG_ENV_SIZE);
	printf("  MMC:      %s\n", CONFIG_MMC ? "y" : "n");
	printf("  NET:      %s\n", CONFIG_NET ? "y" : "n");
#ifdef CONFIG_NET_DHCP
	printf("  DHCP:     y\n");
#else
	printf("  DHCP:     n\n");
#endif
	printf("  SERIAL:   %s\n", CONFIG_SERIAL ? "y" : "n");
	printf("  GPIO:     %s\n", CONFIG_GPIO ? "y" : "n");
	printf("  USB:      %s\n", CONFIG_USB ? "y" : "n");
	return 0;
}
