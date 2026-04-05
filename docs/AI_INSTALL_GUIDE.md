# VMake 安装指南

AI 阅读此文档后，按顺序执行以下命令即可完成安装。

## 前置要求

- Go 1.26+
- GCC/G++ (C/C++ 编译器)

## 安装步骤

### 1. 安装 vmake

```bash
go install gitee.com/spock2300/vmake/cmd/vmake@latest
```

确保 `~/go/bin` 在 PATH 中：

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

如需持久化，将上述命令追加到 shell 配置文件（`~/.bashrc`、`~/.zshrc` 等）。

### 2. 安装命令行补全

vmake 基于 cobra，内置 bash/zsh/fish 补全支持。一条命令自动安装：

```bash
vmake completion install
```

该命令自动检测当前 shell 并安装到对应位置：
- bash: `~/.local/share/bash-completion/completions/vmake`
- zsh: `~/.zsh/completions/_vmake`（自动在 `~/.zshrc` 添加 fpath）
- fish: `~/.config/fish/completions/vmake.fish`

也可手动指定 shell：`vmake completion install --shell bash`

### 3. 安装 AI Skill

Skill 为 AI 编码助手提供 vmake API 知识，辅助编写 `build.go`：

```bash
vmake skill install
```

使用 `--project` 或 `-p` 参数可将 Skill 安装到当前项目的 `.claude/skills/` 目录：

```bash
vmake skill install --project .
```

安装位置：
- `~/.claude/skills/vmake/` — Claude Code
- `~/.agents/skills/vmake/` — OpenCode

包含内容：
- `SKILL.md` — 核心指南（构建阶段、决策树、API 速查、RTOS 支持）
- `references/api.md` — 完整 API 方法签名
- `references/cli.md` — CLI 命令参考
- `examples/` — 各场景 build.go 示例

### 4. 验证

```bash
vmake version
vmake skill path
```
