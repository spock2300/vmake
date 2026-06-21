#ifndef CORE_H
#define CORE_H

#ifdef __cplusplus
extern "C" {
#endif

#define CORE_VERSION   100
#define CORE_BUILD_ID  "v1.0.0"

int core_init(void);
int core_get_version(void);
const char *core_build_id(void);
int core_log(const char *level, const char *msg);
int core_log_set_fd(int fd);
unsigned long core_hash(const void *data, int len);

#ifdef __cplusplus
}
#endif

#endif
