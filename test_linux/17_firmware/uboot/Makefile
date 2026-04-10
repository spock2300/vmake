CC ?= gcc
SCRIPTS_DIR = ../scripts
KCONFIG = Kconfig
DOTCONFIG ?= .config

all: u-boot.bin

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

u-boot.bin: src/main.c include/autoconf.h
	$(CC) -I include -o $@ src/main.c

clean:
	rm -f u-boot.bin
	rm -rf include
	rm -f $(DOTCONFIG) $(DOTCONFIG).old

install:
	@if [ -z "$(DESTDIR)" ]; then echo "Usage: make install DESTDIR=<dir>"; exit 1; fi
	mkdir -p $(DESTDIR)
	cp u-boot.bin $(DESTDIR)/

.PHONY: all silentoldconfig menuconfig savedefconfig clean install
