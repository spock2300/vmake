package main

import (
	"fmt"

	"github.com/spock2300/vmake/pkg/api"
)

func Main(p *api.Package) {
	p.SetRoot(true)
	p.OnConfig(func(ctx *api.ConfigContext) {
		ctx.GlobalOption(api.TargetOSOptionName).SetType(api.OptionString).SetDefault("none")
		ctx.GlobalOption(api.TargetTripleOptionName).SetType(api.OptionString).SetDefault("arm-none-eabi")
		ctx.AddGlobalCFlags("-mcpu=cortex-m4", "-mthumb", "-ffunction-sections", "-fdata-sections")
		ctx.AddGlobalCxxFlags("-mcpu=cortex-m4", "-mthumb", "-ffunction-sections", "-fdata-sections")
		ctx.AddGlobalLdFlags("-mcpu=cortex-m4", "-mthumb", "--specs=nosys.specs", "-Wl,--gc-sections")
	})
	p.OnBuild(func(ctx *api.BuildContext) {
		var definitions []string
		for i := 0; i < 2400; i++ {
			definitions = append(definitions, fmt.Sprintf("RESPONSE_FILE_PROBE_%d=1", i))
		}
		ctx.Target("support").SetKind(api.TargetStatic).
			AddFiles("src/support.cpp").AddDefines(definitions).AddCxxFlags("-fno-exceptions", "-fno-rtti")
		ctx.Target("firmware.elf").SetKind(api.TargetBinary).
			AddFiles("src/main.c", "src/raw.s", "src/startup.S").
			AddIncludes("include").AddDeps("support").
			SetLinkerScript("board.ld").AddLdFlags("-nostdlib").
			AddPostLinkHex().AddPostLinkBin().AddPostLinkSize()
	})
}
