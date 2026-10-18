# VMake 安装指南

AI 阅读此文档后，按顺序执行以下命令即可完成安装。

## 前置要求

- Go 1.26+
- C/C++ 编译器（按平台准备）：
  - Linux：PATH 中的 GCC/G++
  - macOS：Xcode Command Line Tools（提供 `cc`/`c++`），无需 GNU binutils
  - Windows：原生构建使用 MinGW-w64，裸机构建使用 ARM GNU 工具链；此外还需要完整版 Git for Windows，并启用开发者模式（vmake 的存储布局依赖符号链接）

## 安装步骤

### 1. 安装 vmake

```bash
go install github.com/spock2300/vmake/cmd/vmake@latest
```

安装位置为 `$GOBIN`（若已设置），否则为 `$(go env GOPATH)/bin`（默认
`~/go/bin`）；Windows 上文件名为 `vmake.exe`。确保该目录在 PATH 中，默认
GOPATH 布局下可执行：

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

如需持久化，将上述命令追加到 shell 配置文件（`~/.bashrc`、`~/.zshrc` 等）。

### 2. 安装命令行补全

vmake 基于 cobra，可为 bash/zsh/fish/powershell 生成补全脚本；自动安装
仅支持 bash/zsh/fish。一条命令自动安装：

```bash
vmake completion install
```

该命令用 `$SHELL` 的 basename 检测当前 shell 并安装到对应位置：
- bash: `~/.local/share/bash-completion/completions/vmake`
- zsh: `~/.zsh/completions/_vmake`；仅当 `~/.zshrc` 已存在且尚未包含 fpath 行时追加，不会创建 `~/.zshrc`
- fish: `~/.config/fish/completions/vmake.fish`

`$SHELL` 未设置或无法识别时（Windows 上常见），自动检测会失败，请手动指定：
`vmake completion install --shell bash`。PowerShell 没有自动安装器，可用
`vmake completion powershell` 生成脚本后写入 PowerShell profile。

### 3. 安装 AI Skill

Skill 为 AI 编码助手提供 vmake API 知识，辅助编写 `build.go`：

```bash
vmake skill install
```

使用 `--project` 或 `-p` 参数可将 Skill 额外安装到指定目录下的
`.claude/skills/vmake/`。参数按给定路径原样拼接，不会向上查找项目根；
`--project .` 表示当前目录：

```bash
vmake skill install --project .
```

全局安装位置（每次安装都会写入）：
- `~/.claude/skills/vmake/` — Claude Code
- `~/.agents/skills/vmake/` — OpenCode

包含内容：
- `SKILL.md` — 核心指南（构建阶段、决策树、API 速查、RTOS 支持）
- `references/api.md` — 完整 API 方法签名
- `references/cli.md` — CLI 命令参考（安装时从实际命令树生成）
- `references/dirs.md` — 目录与路径解析规则
- `references/gotchas.md` — 高级陷阱与修复模式
- `examples/` — 各场景 build.go 示例

### 4. 验证

```bash
vmake version
vmake skill path
```
