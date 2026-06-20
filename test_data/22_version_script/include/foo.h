#ifndef FOO_H
#define FOO_H

#if defined(_WIN32)
  #if defined(FOO_BUILD_DLL)
    #define FOO_EXPORT __declspec(dllexport)
  #else
    #define FOO_EXPORT __declspec(dllimport)
  #endif
#else
  #define FOO_EXPORT __attribute__((visibility("default")))
#endif

FOO_EXPORT int foo_api(int x);
FOO_EXPORT void foo_init(void);

#endif
