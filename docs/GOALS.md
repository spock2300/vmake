# VMake 长期目标

本文件记录 VMake 的长期目标、验收定义与里程碑。修改目标前先更新本文件。

术语见 [GLOSSARY.md](../GLOSSARY.md)：**采用者**（安装 VMake 的人）、**操作者**（执行构建的 AI agent）、**一次成功**。

## 目标

VMake 是公开的 agent-first C/C++ 构建系统：采用者预置环境，操作者只凭安装好的技能完成构建、测试与安装，并能正确解释构建状态。

护城河是 agent 可操作性——声明式 Go 脚本、可 `inspect` 的状态、路由式技能、机器可读的构建/测试报告。不是"替代 CMake"，不是领域宽度，不是包生态。

## 成功的定义

唯一判定线（M2）：一个外部采用者在 `libwave` 上，由他的操作者在单次会话内完成构建、测试与安装，并正确解释一处构建状态。

- 采用者：反馈回路最短的真实 C/C++ 开发者，优先同事；宿主机 Linux
- 起点：采用者已用 `go install` 装好 VMake、装好目标工具链，并已完成依赖信任
- 信任边界：依赖信任由采用者在会话开始前完成，操作者不得在会话内自行授予信任；供应链决策不交给 agent
- `libwave`：公开、CMake + 测试、此前没有 `build.go`
- 一次成功：零环境修改（不 export `PATH`/`CC`/`CXX`/`AR`/`LD`）、零人工介入、零手工改脚本、状态解释与 `inspect`/报告/`toolchain show` 一致

加固（M3）：`ed25519`——公开、没有任何构建系统——从零建立，证明技能能"建"而不只是"接"。

## 里程碑

| 里程碑 | 内容 | 判定 |
| --- | --- | --- |
| M0 | 干净 `HOME`、只装技能，跑 `compiler`、`multi`、`inspect-report` 三个任务 | 三个任务有记录（[RESULTS.md](../scripts/skill-loop/RESULTS.md)），通过率时间序列有基线 |
| M1 | 公开面最小修正（GitHub description/topics、README 首句、`VMAKE_INTRODUCTION.md` 改写或下线）、`vmake skill install` 版本绑定、Linux/macOS 自动 CI | 上述各项完成 |
| M2 | 外部采用者 + `libwave` | 成功定义达成（唯一算"赢"的线） |
| M3 | `ed25519` 从零建 | 成功定义在无构建系统项目上复现 |
| M4 | Windows 真机验证；package/target 构建过滤（文档化 gap → feature） | 按需 |

## 接口

- 技能 = 路由表 + 不变量；可被命令验证的事实必须能由命令输出复现，不能复现的才留在文档里
- `vmake skill install` 写入二进制版本；`vmake doctor` 对不匹配的安装副本报错
- 不做 MCP：那是一个新的协议支持面与兼容承诺

## 兼容

- 内部规则：1.x 内脚本 API 与 CLI 只增不改——不删除、不改现有语义；移除只发生在 2.0（DD-34）
- 公开承诺：等第一个外部采用者出现后再升格，当前不在 README 承诺

## 平台

| 宿主 | 状态 | 验证 |
| --- | --- | --- |
| Linux | supported | 自动 CI |
| macOS | supported | 自动 CI |
| Windows | experimental | wine（`deepin-wine8-stable`）冒烟，进 release checklist；真机按需 |

## 生态

registry 是格式不是服务：采用者自带 wrapper（BYO）。三个以上采用者在包同一个库时，再考虑收编为官方 registry。

## 非目标

- 永不：IDE 插件、GUI、分布式构建（与"单机、可解释"直接冲突）
- 暂缓：MSVC（采用者落在 Windows + MSVC 时重排）
- 路线图：package/target 构建过滤（当前是文档化 gap，M4）

## 限制处置

- 修（已完成）：本地 `build.go` 重名从静默丢弃改为硬错误（DD-33）
- 接受并文档化：registry 命名歧义、父包不能在自己的 `OnRequire` 中引用子包、子包扫描复用 `skipDirs`

## 内部约定（不对外发布）

- 时间盒只约束输入：M0 ≤ 2026-11-22，M1 ≤ 2026-12-15；M2 不设到期日
- 降级条件：2027-05-15 前没有外部采用者，公开面标 experimental 并停止新增公开承诺；技能循环通过率连续两个季度不升，冻结新功能、只修 bug
- 发版闸门：改动 CLI/API/技能/错误文本时跑 `compiler`、`multi`、`inspect-report` 与四个 `factcheck-*`，记录每次通过数/总数
