# Windows ARM 构建验证

本轮支持 Windows x64 主机构建裸机 / RTOS，使用 GNU ARM 工具链生成 ELF、HEX 和 BIN。按任务要求，不修改子图实现，不在 Windows 验证子图。

## 工具链与扩展

`vmake-tools` 仓库只提供 `arm-none-eabi/toolchain.json`，vmake 自身扫描并注册，不需要插件。定义根据运行主机的 `GOOS/GOARCH`，从 `installations` 中选择 `linux/amd64` 或 `windows/amd64`。目标平台由项目声明：`test_windows/arm_firmware/build.go` 在 `OnConfig` 中设置全局选项 `target_os = "none"`、`target_triple = "arm-none-eabi"`，并通过 `AddGlobalCFlags`／`AddGlobalLdFlags` 提供 `-mcpu=cortex-m4 -mthumb --specs=nosys.specs` 等 CPU 选项，与主机平台和工具链定义分开。

Windows 资产为 `arm-gnu-toolchain-15.3.rel1-mingw-w64-x86_64-arm-none-eabi.zip`，SHA256 为 `b85669d3408e2ae713b17b0cc59bc4ea26369a7f2bd19108fd11df7095f159e6`。Linux TAR 与 Windows ZIP 均保留，Git LFS 按平台只取所需资产。两份插件目录已同步：`examples/plugins` 和 `/home/spock/.vmake/extensions/vmake-tools`。

安装目录为 `~/.vmake/toolchains/<os>/<arch>/<name>/<version>`。解压、哈希和工具校验成功后才发布目录。旧的安装目录保留；旧 `host`、`install` 字段以及 `target_os`、`target_triple`、`default_flags` 均需要按 README 迁移，后三者移入项目 `build.go`。

## 已执行的验证

环境为 Linux、Go 1.27.1、`CGO_ENABLED=0`、deepin-wine11-stable 11.0，以及上述 Windows/Linux ARM GNU 15.3.rel1 编译器。

- Linux 与 Windows/amd64 的 vmake 均编译成功，交付 EXE 的 Go 构建信息确认 `CGO_ENABLED=0`。
- Linux 作用域单元测试和 vet；Linux ARM 构建、安装与增量回归。
- Wine 下实际选择并安装 Windows ZIP，编译 C、C++、`.s`、`.S`、静态库，使用链接脚本生成 ELF、HEX、BIN。
- 验证无修改时跳过、头文件和汇编包含文件修改后重编译、链接脚本修改后重新链接、删除 HEX/BIN 后恢复、rebuild 和 distclean。
- 中文、空格工程目录和超过 260 字符的产物路径；2400 个独立宏定义令编译参数超过 32 KB，通过 response file 执行。
- Windows 文件/目录链接、错误链接类型修复、循环检测、目录复制、平台脚本筛选、脚本哈希、深层包名和跨进程文件锁回归。
- Windows Git 冷克隆、Git 跟踪的符号链接、离线共享缓存、固定提交校验、源码刷新和不可变补丁副本测试，均无跳过。Wine 中 MSYS shell 无法正常运行，此项使用临时目录里的官方 BusyBox MinGit 完成，生产代码未增加绕过。
- Linux 快照全量比较通过；预期变化来自编译数据库新增 `arguments`、工具链指纹变化和已迁移的构建脚本，基线已同步。
- 真实 GNU Make 验证含空格工具路径、交叉编译前缀、EnsureConfig、CleanContext.Make 和 Configure；`Package.Env()` 继续返回原始路径。

固定 ARM 测试位于 `test_windows/arm_firmware`；验证入口为 `test_windows/verify_arm.py`。`arm_expected.json` 固定入口地址 `0x08000001` 及 BIN/HEX 哈希；Linux 与 Wine 生成的烧录文件哈希一致。

## 2026-09-22 review 修复回归

本次在已有修改上修复 8 项 review 问题，并保留双平台 ARM 工具链分发配置。构建和测试均使用 `CGO_ENABLED=0`。

| 修复项 | 验证内容 |
| --- | --- |
| 自复制保护 | 同路径、软链接及硬链接别名均报错且原内容不变；普通覆盖无尾部残留；拒绝源目录链接进入目标树 |
| 工具链错误隔离 | 健康、旧版、损坏及不支持当前宿主的 manifest 共存；健康选择成功，错误选择和 query 明确失败；内置 ext 跳过插件执行，前置全局参数及真实本地 Git update/remove 可用 |
| Windows CI 执行目录 | prerequisites 在 `test_data/01_simple_c` 执行 `../../vmake.exe doctor --toolchain host`；保留原生及 ARM 后续步骤 |
| Clang 汇编 | Linux 上实际运行 GCC/Clang `.s`、`.S`，验证无修改跳过、嵌套 `.include` 和预处理依赖的修改及删除、同名源文件并行编译、失败清理和编译数据库；检查 COFF/ELF 对象格式 |
| make 按需解析 | 无默认 make 时普通编译可用；显式 MAKE 仍严格校验；doctor 提示需要 make 的具体操作 |
| 断链过滤 | glob 和过滤复制跳过无关断链；匹配或保留的断链、目录循环仍报错 |
| 显式 post-link 产物 | debuglink 连续构建不重链；删除声明的 DEBUG/HEX/BIN/STRIP 后恢复；安装只包含声明的额外产物 |
| 自定义 menuconfig | 已有配置时自定义程序无需 make，独立 argv 和 SrcDir 保持；生成 preset 或默认菜单才解析 make |

Linux 作用域单测与 vet 通过：

```bash
CGO_ENABLED=0 go test ./cmd/vmake/... ./pkg/... ./internal/...
CGO_ENABLED=0 go vet ./cmd/... ./pkg/... ./internal/...
```

Wine 下重新运行了文件系统、glob、复制、工具链定义隔离、扩展恢复、doctor 及 TUI 定向测试。新增回归均通过；已有 `TestEnsureConfigUsesSelectedMakeInsteadOfMenuconfig` 因使用 POSIX 测试脚本，在 Windows 按设计跳过，Linux 已覆盖。Windows ARM 全套验证再次通过，包括中文空格目录、248 个 UTF-16 单元的进程工作目录、超过 260 字符的产物路径和超过 32 KB 的编译参数；Linux ARM 也再次通过。Windows 验证未运行子图。

交叉 review 另外修正了 Windows 上 `./{output}.bin` 的模板展开：执行命令、增量检查和安装共用同一展开规则。Windows 定向回归验证现有产物不重链、额外产物安装成功、删除后触发重链，并排除重复声明的主产物。

Linux snapshot 全量比较通过。本次基线更新来自移除内置 `MAKE="make"` 后的工具链标识与 buildKey 变化，编译参数本身未变。项目 22 与 firmware17 把依赖共享库的绝对路径写入 ELF 的 `DT_NEEDED`，因此部分二进制哈希也变化。使用仅恢复旧 MAKE 字段的二进制做同源 A/B，确认这些 ELF 仅 `.dynstr` 和 `.note.gnu.build-id` 不同；归一化 buildKey/build-id 后完整文件一致，编译数据库也精确复现旧、新基线哈希。未修改子图实现；Linux 子图快照仅随通用 buildKey 变化更新。

自定义 `AddPostLink` 需要用 `AddPostLinkOutputs` 声明额外产物，才能获得缺失产物恢复与自动安装。HEX/BIN/Strip 辅助方法已自动声明；命令中的 `--add-gnu-debuglink={output}.debug` 不再被当作输出声明。Clang 必须使用其驱动支持的外部 GNU assembler，通过现有工具链参数配置 `--target` 和 `-B`；本次不扩展 Clang 裸机支持，Windows ARM 继续使用 GNU ARM GCC。

Deepin Wine 会缓存同一路径下的 PE 镜像。本次替换 EXE 后曾在 Go 入口之前因旧缓存启动失败；保留并移走专用 Wine prefix 的 `.cache` 后，原路径的最终 EXE 正常运行并通过 ARM 验证。生产代码未因此增加兼容绕过。复测同一路径下的新 EXE 时应使用干净的 Wine 镜像缓存。

## CMake API 通用封装回归

CMake 的 configure、build、install 统一使用 `CMakeBuildDir()` 和 `CMakeInstallDir()`，默认构建目录为 `BuildDir/cmake`。本地安装使用 `BuildDir/staging`，远程包保留现有安装前缀。构建脚本通过 setter 修改目录和默认配置，通用编译器解析、ASM 驱动、工程全局 flags、并行及安装参数由 API 管理。内置 skill、API 文档和第三方库示例均优先使用这三个 API。

Linux 上使用真实 CMake 3.31.4 和 Ninja 运行 `python3 test_windows/verify_cmake.py ./vmake --arm`，验证了中文空格目录中的 C/CXX/ASM、静态库与 MODULE 库、可执行文件、安装、增量跳过和输入修改后的重建。覆盖 Ninja、显式配置集合的 Ninja Multi-Config、configure preset，以及 GNU ARM 裸机静态库；检查了产物的 ELF 架构和 VMake prebuilt 发布目录隔离。多配置生成器的可用配置集合由工程或 preset 声明，API 只统一选择配置。CMake build preset 会覆盖显式构建目录，因此 API 明确拒绝该参数，保留 configure preset 支持。

HK MCU SDK 的 libc 已使用通用 CMake API。原 Wi-Fi/BLE 配置及隔离副本中的 LVGL Widgets/QSPI 配置均完成 `distclean`、构建安装及增量检查；原配置、vendor 哈希及已有 picolibc 补丁保持不变。固件 ELF/HEX/BIN、资源和 demo 检查通过。Linux 作用域单测、vet 和 snapshot 通过，snapshot 基线无需更新。

`/home/spock/.vmake/repos/official` 的七个 CMake wrapper 已同步：cjson、curl、libsrtp2、mbedtls、usrsctp、wolfssl、zlib。移除手工并行参数，cjson 使用安装目录 getter，usrsctp 的局部 C flags 与工程全局 flags 合并。隔离缓存中的 cjson、curl、libsrtp2、mbedtls、usrsctp、zlib 构建、安装及增量检查通过。wolfssl 5.7.2 在 GCC 12.3 下于 `src/tls.c:1066` 触发 `-Werror=stringop-overflow`；同源码、原功能选项和旧构建参数也复现。仅在独立诊断工程加入 `-Wno-error=stringop-overflow` 后，经未修改的官方 wrapper 完成 API 三阶段及增量验证；官方配置和第三方源码未加入该抑制措施，默认构建仍受此既有问题影响。

Windows EXE 使用 `CGO_ENABLED=0` 交叉编译。Wine 下复跑 ARM 固件、中文空格与长路径、超过 32 KB 参数、安装和增量验证，CMake API 定向单测也单独验证 Windows 路径行为。Wine 未用于实际运行 Windows CMake；原生 Windows workflow 新增 `verify_cmake.py ./vmake.exe --arm`，覆盖 CMake 的 native 与 ARM 三阶段构建。该 CI 本轮未触发，仍须独立验收。

## 原生 Windows CI

`.github/workflows/windows.yml` 使用固定版本及 SHA256 的官方 ARM 资产，安装所需 MinGW/MSYS 工具，关闭 CGO，运行单元测试和 ARM 验证，并排除子图测试。本轮没有触发远程 CI；Wine 结果不等同于 Windows 真机验收。

Windows 仍需要开发者模式或软链接权限。长路径验收覆盖产物路径；启动外部进程的工作目录需保持在 Windows `MAX_PATH` 范围内，Wine 对超过该范围的工作目录也会启动失败。本轮验证了中文工程根目录，未扩展为任意 Unicode 源文件名或任意第三方工具的编码保证。
