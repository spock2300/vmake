#include "chip.h"

static int regs[16];

void chip_init(void) {
    for (int i = 0; i < 16; i++) {
        regs[i] = 0;
    }
}

int chip_read_reg(int addr) {
    if (addr < 0 || addr >= 16) return -1;
    return regs[addr];
}

void chip_write_reg(int addr, int val) {
    if (addr >= 0 && addr < 16) {
        regs[addr] = val;
    }
}
