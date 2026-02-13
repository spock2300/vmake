# VMake - AGENTS.md

AI 编码代理在 VMake 代码库中工作时的重要设计原则和约定。

## 核心设计哲学

**少即是多 (Less is More)**

VMake 的核心设计原则是极简主义。每添加一个新功能或 API，都需要回答：
- 这是绝对必要的吗？
- 用户 80% 的场景是否只需要 20% 的功能？
- 能否通过更简单的方式达成目标？

## 架构决策

### 为什么使用 Go 插件而非自定义 DSL

- **类型安全**: Go 编译器保证配置代码的正确性
- **工具链成熟**: IDE 支持、自动补全、静态分析开箱即用
- **零学习成本**: 用户只需懂 Go，无需学习新语言
- **权衡**: 插件兼容性要求（相同 Go 版本编译）

### 为什么分离 OnConfig 和 OnBuild

- **关注点分离**: 配置收集与构建执行是两个独立阶段
- **延迟求值**: 配置阶段收集选项，构建阶段才读取用户选择的值
- **TUI 交互**: 需要先收集所有选项才能渲染界面

### 为什么 Target API 只有 11 个方法

经过精简，删除了以下方法：
- `AddSysIncludes`, `AddSysLinks` - 可用 `AddIncludes` + `-isystem` flag 替代
- `AddUndefines` - 极少使用
- `AddExcludes` - 可通过 glob 模式排除
- `AddFrameworks` - macOS 特有，后续按需添加
- `SetPrecompiledHeader` - 增加复杂度，暂不支持
- `SetOutputDir`, `SetSourceDir` - 使用默认目录结构

## API 设计原则

### 方法链 (Fluent API)

所有 API 使用链式调用，支持声明式配置：

```go
ctx.Target("app").
    SetKind(api.TargetBinary).
    AddFiles("src/*.c").
    AddIncludes("include")
```

### 命名约定

- **SetXxx**: 设置单一值 (SetKind, SetDefault, SetLanguages)
- **AddXxx**: 添加多个值 (AddFiles, AddIncludes, AddDefines)
- 参考 xmake 命名风格，但遵循 Go 惯例

### 类型定义

使用类型别名增强可读性和类型安全：

```go
type TargetKind string  // 而非 string
type OptionType int     // 而非 int
type ConfigFunc func(ctx *ConfigContext)  // 而非裸函数类型
```

## 包职责划分

| 包 | 职责 | 可被插件导入 |
|---|---|---|
| `pkg/api` | 核心 API (Builder, Target, Option) | 是 |
| `pkg/plugin` | 插件扫描、编译、加载 | 否 |
| `pkg/config` | 配置存储 | 否 |
| `pkg/tui` | 终端 UI | 否 |
| `pkg/build` | 构建执行 | 否 |
| `pkg/toolchain` | 工具链抽象 | 否 |
| `internal/*` | 内部实现细节 | 否 |

**原则**: `pkg/api` 是唯一需要保持稳定的公开 API。

## 执行流程

config 和 build 命令共享相同的前置流程，仅在最后一步分叉：

### 统一前置流程

```
Scan → Compile → Load → OnConfig → 加载已有配置值
```

1. 扫描 build.go 文件
2. 编译为插件
3. 加载插件
4. 执行 OnConfig 收集选项定义
5. 从 `.vmake/config.json` 加载已有配置值

### 命令分叉

| 命令 | 后续操作 |
|------|---------|
| `vmake config` | 渲染 TUI → 用户配置 → 保存配置 |
| `vmake` / `vmake build` | OnBuild → 生成 Target → 编译链接 |

**默认构建模式**：直接运行 `vmake` 等同于 `vmake build`

### 设计理由

- **OnConfig 始终执行**：收集选项定义，用已有值填充
- **config 命令**：允许用户修改配置
- **build 命令**：使用已有配置值直接构建

## 条件表达式设计

使用函数式条件表达式而非 if 语句：

```go
// 好的方式
AddDefines(ctx.If("debug", "DEBUG=1"))

// 避免
if ctx.Bool("debug") {
    AddDefines("DEBUG=1")
}
```

原因：声明式风格更易于分析和优化，且与链式调用风格一致。

## 错误处理原则

- 库代码不 panic，始终返回 error
- 错误信息包含上下文：`"failed to open plugin %s: %w"`
- 使用 `errors.Is` / `errors.As` 进行错误判断

## 不做什么

以下功能明确不在当前范围内：
- 包管理器 (参考 xmake 的 package 管理)
- IDE 集成插件
- 远程构建
- 分布式编译

## 参考

详细 API 设计见 `docs/API_DESIGN.md`
