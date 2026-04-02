extern int get_magic_value(void);
extern void print_info(void);

int main(void) {
	print_info();
	return get_magic_value() == 126 ? 0 : 1;
}
