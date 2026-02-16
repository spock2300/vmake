# VMake - AGENTS.md

AI 编码代理在 VMake 代码库中工作时的重要设计原则和约定。

## Build / Lint / Test Commands

```bash
# Build vmake binary
go build -o vmake ./cmd/vmake

# Format code
gofmt -w .

# Run manual tests (no unit tests exist)
cd test_data/01_simple_c && ../../vmake build
cd test_data/08_with_package && ../../vmake build

# Clean and rebuild
./vmake clean && ./vmake build
```

## 核心设计哲学

**少即是多 (Less is More)**

每添加一个新功能或 API，都需要回答：
- 这是绝对必要的吗？
- 用户 80% 的场景是否只需要 20% 的功能？
- 能否通过更简单的方式达成目标？

## Official Repository

`official_repo/` 是一个**独立的 git 仓库**，用于索引和管理第三方包。
- 路径格式：`{repo_name}/{package_name}` (如 `official/zlib`)
- 包定义位于 `official_repo/packages/{first_letter}/{package_name}/package.go`

## Code Style

### No Comments

**禁止添加任何注释**，除非用户明确要求。代码应自解释。

### Imports Ordering

```go
import (
    // 1. Standard library
    "context"
    "fmt"
    "os/exec"
    
    // 2. External packages
    "github.com/spf13/cobra"
    
    // 3. Local packages
    "gitee.com/spock2300/vmake/pkg/api"
)
```

### Naming Conventions

- **SetXxx**: 设置单一值 (SetKind, SetDefault, SetLanguages)
- **AddXxx**: 添加多个值 (AddFiles, AddIncludes, AddDefines)
- **类型别名**: 使用类型别名增强可读性
  ```go
  type TargetKind string  // 而非 string
  type OptionType int     // 而非 int
  type ConfigFunc func(ctx *ConfigContext)  // 而非裸函数类型
  ```

### Fluent API

所有 API 使用链式调用：

```go
ctx.Target("app").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddIncludes("include")
```

### Error Handling

- 库代码不 panic，始终返回 error
- 错误信息包含上下文：
  ```go
  return fmt.Errorf("git clone %s -> %s: %w", url, dir, err)
  return fmt.Errorf("failed to find package %s: %w", name, err)
  ```
- 使用 `%w` 包装错误，支持 `errors.Is` / `errors.As`

## Package Structure

| 包 | 职责 | 可被插件导入 |
|---|---|---|
| `pkg/api` | 核心 API (Builder, Target, Option, Package) | **是** |
| `pkg/plugin` | 插件扫描、编译、加载 | 否 |
| `pkg/config` | 配置存储 | 否 |
| `pkg/tui` | 终端 UI | 否 |
| `pkg/build` | 构建执行、编译、链接 | 否 |
| `pkg/toolchain` | 工具链抽象 (GCC, Clang) | 否 |
| `pkg/repo` | 包管理、Git 操作、依赖解析 | 否 |
| `pkg/log` | 日志系统 | 否 |
| `internal/*` | 内部实现细节 | 否 |

**原则**: `pkg/api` 是唯一需要保持稳定的公开 API。

## 执行流程

```
Scan → Compile → Load → OnConfig → 加载已有配置值
                                    ↓
                          ┌─────────┴─────────┐
                          ↓                   ↓
                    vmake config        vmake / vmake build
                    渲染 TUI            OnBuild → 生成 Target → 编译链接
```

### Package Management Flow

```
OnRequire → Resolver → Collect Constraints → Load Package Definitions → 
SelectVersion(constraint) → IsInstalled? → (no) EnsureSource → InstallPackage → Build
```

### Config Storage

- 本地包选项 → `config.Packages[pkgName].options`
- 第三方包 (名称含 `/`) → `config.Requires[pkgName]`
- 全局选项 → `config.Global.Options`

## 条件表达式

使用函数式条件表达式而非 if 语句：

```go
// 好的方式
AddDefines(ctx.If("debug", "DEBUG=1"))

// 避免
if ctx.Bool("debug") {
    AddDefines("DEBUG=1")
}
```

## 不做什么

- IDE 集成插件
- 远程构建
- 分布式编译
- MSVC 工具链 (暂不支持)

## 参考

- 详细 API 设计: `docs/API_DESIGN.md`
- 测试项目: `test_data/01_simple_c` 到 `test_data/08_with_package`
