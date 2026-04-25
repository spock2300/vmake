#ifndef CHIP_H
#define CHIP_H

void chip_init(void);
int chip_read_reg(int addr);
void chip_write_reg(int addr, int val);

#endif
