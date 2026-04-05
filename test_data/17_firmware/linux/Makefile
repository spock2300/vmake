CC ?= gcc
SCRIPTS_DIR = ../scripts
KCONFIG = Kconfig
DOTCONFIG ?= .config

all: zImage dtbs

%_defconfig:
	cp configs/$@ $(DOTCONFIG)
	@$(MAKE) --no-print-directory silentoldconfig

silentoldconfig:
	@mkdir -p include
	@bash $(SCRIPTS_DIR)/gen-autoconf.sh $(DOTCONFIG) include/autoconf.h

menuconfig:
	@bash $(SCRIPTS_DIR)/kconfig-menu.sh $(KCONFIG) $(DOTCONFIG)
	@$(MAKE) --no-print-directory silentoldconfig

savedefconfig:
	@cp $(DOTCONFIG) configs/defconfig

vmlinux: src/vmlinux.c include/autoconf.h
	$(CC) -I include -o $@ src/vmlinux.c

zImage: vmlinux
	cp vmlinux zImage

dtbs: arch/dts/board.dtb

arch/dts/board.dtb: arch/dts/gen_dtb.c include/autoconf.h
	@mkdir -p arch/dts
	$(CC) -I include -o arch/dts/_gen_dtb arch/dts/gen_dtb.c
	arch/dts/_gen_dtb $@
	rm -f arch/dts/_gen_dtb

clean:
	rm -f vmlinux zImage
	rm -f arch/dts/board.dtb arch/dts/_gen_dtb
	rm -rf include
	rm -f $(DOTCONFIG) $(DOTCONFIG).old

install:
	@if [ -z "$(DESTDIR)" ]; then echo "Usage: make install DESTDIR=<dir>"; exit 1; fi
	mkdir -p $(DESTDIR)/dts
	cp zImage $(DESTDIR)/
	cp arch/dts/board.dtb $(DESTDIR)/dts/

.PHONY: all silentoldconfig menuconfig savedefconfig zImage dtbs clean install
