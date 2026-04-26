#include "chip.h"

#ifdef CONFIG_CHIP_FEATURE_A
int chip_get_mode(void) { return 1; }
#else
int chip_get_mode(void) { return 0; }
#endif

#ifdef CONFIG_CHIP_MODE_FAST
int chip_speed(void) { return 100; }
#elif defined(CONFIG_CHIP_MODE_SLOW)
int chip_speed(void) { return 50; }
#else
int chip_speed(void) { return 25; }
#endif
