# VMake Test Data

此目录包含用于 VMake 开发和测试的示例项目。

## 目录结构

```
test_data/
├── 01_simple_c/          # 简单 C 项目
│   ├── build.go
│   └── src/main.c
│
├── 02_with_config/       # 带配置选项的项目
│   ├── build.go
│   └── src/
│
├── 03_multi_target/      # 多目标项目（库+应用+测试）
│   ├── build.go
│   ├── include/mylib.h
│   ├── src/main.c
│   ├── src/mylib.c
│   └── tests/test_mylib.c
│
├── 04_multi_module/      # 多模块项目
│   ├── build.go          # 根配置
│   ├── include/utils.h
│   ├── lib/
│   │   ├── build.go
│   │   └── utils.c
│   └── app/
│       ├── build.go
│       └── main.c
│
├── 05_conditional/       # 条件表达式示例
│   ├── build.go
│   └── src/
│
├── 06_complete_api/      # 完整 API 测试
│   ├── build.go
│   ├── include/
│   └── src/
│
├── 07_subbuild_codegen/  # 子构建 / 代码生成
│   ├── build.go
│   ├── tools/
│   ├── src/
│   └── output/
│
├── 08_with_package/      # 使用第三方包
│   ├── build.go
│   └── src/
│
├── 09_with_curl/         # 远程包依赖示例（curl + mbedtls）
│   ├── build.go
│   ├── go.mod
│   └── src/
│
├── 10_local_repo/        # 本地包仓库
│   ├── app/
│   └── mylib/
│
├── 11_with_tinyexpr/     # 使用 tinyexpr 库
│   ├── build.go
│   └── src/
│
├── 12_rtos_simulate/     # RTOS 模拟（链接脚本 + 后链接步骤）
│   ├── build.go
│   ├── include/
│   ├── linker/
│   └── src/
│
├── 13_with_prefix_repo/  # Native 仓库依赖
│   ├── build.go
│   └── src/
│
└── 14_bin_header/        # 二进制头文件嵌入
    ├── build.go
    ├── assets/
    └── src/
```

## 测试场景说明

### 01_simple_c
最简单的项目，无配置选项，单个目标。

**验证要点：**
- 插件扫描和加载
- 基本的 Target API
- glob 模式匹配 (src/*.c)

### 02_with_config
包含配置选项的项目。

**验证要点：**
- Option API (Bool, Choice)
- OnConfig 回调
- 配置值的读取
- 条件编译 (If)

### 03_multi_target
多目标项目，展示库、应用、测试的典型组合。

**验证要点：**
- 多 Target 注册
- AddDeps 依赖关系
- SetDefault(false) 非默认目标
- 静态库编译

### 04_multi_module
多模块项目，每个子目录有自己的 build.go。

**验证要点：**
- 递归扫描 build.go
- 跨模块依赖
- 配置继承

### 05_conditional
条件表达式综合示例。

**验证要点：**
- If / IfNot 条件
- Select 映射
- 复杂的编译选项组合

### 06_complete_api
完整 API 覆盖测试，包含全局选项、对象文件目标、共享库、条件目标等。

**验证要点：**
- GlobalOption、OptionString、OptionInt
- SetShowIf 条件显示
- TargetObject、TargetShared、TargetStatic
- SetLanguages、AddCxxFlags、AddLdFlags
- When、Bool 条件判断

### 07_subbuild_codegen
子图构建与代码生成，构建宿主工具然后执行生成源文件。

**验证要点：**
- BuildSubGraph 子图构建
- DepOutput 获取依赖输出路径
- Exec 执行构建产物
- ToolchainOption 工具链选项

### 08_with_package
使用第三方包依赖（zlib）。

**验证要点：**
- OnRequire + AddRequires 声明依赖
- AddDeps 链接第三方包
- 版本约束

### 09_with_curl
远程包依赖示例，使用 curl 库（依赖 mbedtls SSL 后端）。

**验证要点：**
- 远程包依赖声明 (OnRequire + AddRequires)
- 版本约束 (>=8.5)
- 传递依赖解析 (curl -> mbedtls)
- CMake 包构建
- AddDeps 链接

### 10_local_repo
本地包仓库，多模块项目间通过本地仓库共享包。

**验证要点：**
- 本地仓库配置
- 包发现与版本匹配
- 跨项目依赖

### 11_with_tinyexpr
使用 tinyexpr 数学表达式库。

**验证要点：**
- 第三方包依赖
- TargetVoid + SetBuildFunc

### 12_rtos_simulate
RTOS/嵌入式固件模拟，展示链接脚本和后链接步骤。

**验证要点：**
- SetLinkerScript 链接脚本
- AddPostLinkHex/AddPostLinkBin/AddPostLinkSize
- AddBinHeader 二进制头文件嵌入

### 13_with_prefix_repo
Native 仓库依赖，通过 git tag 进行版本管理。

**验证要点：**
- Native 仓库配置
- 自动版本发现（git tag）
- Native 包依赖解析

### 14_bin_header
二进制文件嵌入为 C 头文件。

**验证要点：**
- AddBinHeader API
- 增量编译（mtime 判断）
- 自动生成头文件

## 预期行为

每个测试目录都应能执行以下命令：

```bash
cd test_data/01_simple_c
../../vmake build     # 应编译生成 hello 可执行文件
./build/host-debug/hello  # 输出 "Hello, VMake!"
```
