package main

import "gitee.com/spock2300/vmake/pkg/api"

func Main(b *api.Builder) {
	b.OnConfig(func(ctx *api.ConfigContext) {
		ctx.Option("debug").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable debug mode").
			SetGroup("General")

		ctx.Option("verbose").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable verbose output").
			SetGroup("General")

		ctx.Option("c++standard").
			SetType(api.OptionChoice).
			SetDefault("c++17").
			SetValues("c++11", "c++14", "c++17", "c++20").
			SetDescription("C++ standard version").
			SetGroup("C++")

		ctx.Option("optimization").
			SetType(api.OptionChoice).
			SetDefault("O2").
			SetValues("O0", "O1", "O2", "O3", "Os").
			SetDescription("Optimization level").
			SetGroup("General")

		ctx.Option("ssl").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Enable SSL support").
			SetGroup("SSL")

		ctx.Option("ssl_version").
			SetType(api.OptionString).
			SetDefault("1.1.1").
			SetDescription("SSL library version").
			SetGroup("SSL").
			SetShowIf(func(c *api.ConfigContext) bool {
				return c.Bool("ssl")
			})

		ctx.Option("thread_count").
			SetType(api.OptionInt).
			SetDefault(4).
			SetDescription("Number of worker threads").
			SetGroup("Performance")

		ctx.Option("shared_lib").
			SetType(api.OptionBool).
			SetDefault(false).
			SetDescription("Build as shared library").
			SetGroup("Build")

		ctx.Option("custom_prefix").
			SetType(api.OptionString).
			SetDefault("/usr/local").
			SetDescription("Installation prefix").
			SetGroup("Installation")
	})

	b.OnBuild(func(ctx *api.BuildContext) {
		threads := ctx.Int("thread_count")
		prefix := ctx.String("custom_prefix")
		cppStd := ctx.String("c++standard")

		ctx.Target("core_obj").
			SetKind(api.TargetObject).
			AddFiles("src/core.cpp").
			AddIncludes("include").
			SetLanguages(cppStd).
			AddCxxFlags("-Wall", "-Wextra").
			AddCxxFlags(ctx.Select("optimization", map[string]string{
				"O0": "-O0", "O1": "-O1", "O2": "-O2", "O3": "-O3", "Os": "-Os",
			})).
			AddCxxFlags(ctx.If("debug", "-g", "-DDEBUG"))

		ctx.Target("utils_obj").
			SetKind(api.TargetObject).
			AddFiles("src/utils.cpp").
			AddIncludes("include").
			SetLanguages(cppStd)

		if ctx.When("shared_lib", true) {
			ctx.Target("mylib").
				SetKind(api.TargetShared).
				AddFiles("src/library.cpp").
				AddIncludes("src/internal").  // 私有头文件
				AddPublicIncludes("include"). // 公开头文件，依赖方继承
				SetLanguages(cppStd).
				AddDeps("core_obj", "utils_obj").
				AddDefines(ctx.If("ssl", "USE_SSL")).
				AddDefines("THREAD_COUNT=" + string(rune(threads+'0'))).
				AddCxxFlags("-fPIC").
				AddLdFlags("-Wl,-soname,libmylib.so")
		} else {
			ctx.Target("mylib").
				SetKind(api.TargetStatic).
				AddFiles("src/library.cpp").
				AddIncludes("src/internal").  // 私有头文件
				AddPublicIncludes("include"). // 公开头文件，依赖方继承
				SetLanguages(cppStd).
				AddDeps("core_obj", "utils_obj").
				AddDefines(ctx.If("ssl", "USE_SSL")).
				AddDefines("THREAD_COUNT=" + string(rune(threads+'0')))
		}

		ctx.Target("myapp").
			SetKind(api.TargetBinary).
			AddFiles("src/main.cpp").
			SetLanguages(cppStd).
			AddDeps("mylib").
			AddDefines("PREFIX=\"" + prefix + "\"").
			AddDefines(ctx.If("ssl", "USE_SSL")).
			AddDefines(ctx.If("ssl", "SSL_VERSION=\""+ctx.String("ssl_version")+"\"")).
			AddCxxFlags(ctx.If("debug", "-g", "-fsanitize=address")).
			AddCxxFlags(ctx.If("verbose", "-v")).
			AddLinks(ctx.If("ssl", "ssl", "crypto")).
			AddLdFlags(ctx.If("debug", "-fsanitize=address")).
			AddLdFlags("-Wl,--as-needed")

		ctx.Target("benchmark").
			SetKind(api.TargetBinary).
			AddFiles("src/benchmark.cpp").
			SetLanguages(cppStd).
			AddDeps("core_obj").
			AddCxxFlags("-O3", "-DNDEBUG").
			SetDefault(false)

		if ctx.Bool("verbose") {
			ctx.Target("debug_info").
				SetKind(api.TargetBinary).
				AddFiles("src/debug.cpp").
				AddDefines("VERBOSE_MODE").
				SetDefault(false)
		}
	})
}
