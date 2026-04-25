#include <stdio.h>
#include "firmware.h"
#include "chip.h"

static volatile int tick_count = 0;

void timer_isr(void) {
    tick_count++;
}

int main(void) {
    printf("[RTOS SIM] Firmware starting...\n");

#ifdef SIM_V1
    printf("[RTOS SIM] Chip config: SIM_V1\n");
#elif defined(SIM_V2)
    printf("[RTOS SIM] Chip config: SIM_V2\n");
#else
    printf("[RTOS SIM] Chip config: unknown\n");
#endif

    chip_init();
    chip_write_reg(0, 0xAB);
    printf("[RTOS SIM] reg0 = 0x%X\n", chip_read_reg(0));

    for (int i = 0; i < 3; i++) {
        timer_isr();
        printf("[RTOS SIM] Tick %d\n", tick_count);
    }

    printf("[RTOS SIM] LED blink task running\n");
    printf("[RTOS SIM] firmware_version = %s\n", FIRMWARE_VERSION);
    return 0;
}
