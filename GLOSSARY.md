# VMake

VMake 用 Go 脚本构建 C/C++ 工程：工程用声明式脚本描述目标、依赖、配置与工具链，由 VMake 解释执行。

## Language

**采用者** (adopter):
安装 VMake 的人，负责工具链、依赖信任与配置取舍。
_Avoid_: 用户, 客户, 使用者

**操作者** (operator):
执行构建的 AI agent，是技能的主要读者。
_Avoid_: 用户, 助手, bot

**一次成功** (one-shot success):
采用者预置环境后，操作者在一次会话内完成构建、测试与安装，不修改环境、不请求人工介入，并能正确解释构建状态。
_Avoid_: 零配置, 自动构建, 开箱即用

**构建报告** (build report):
一次构建或测试面向操作者的结构化结果记录，描述执行状态、诊断与产物摘要。
_Avoid_: 构建日志, 清单, manifest

**安装清单** (install manifest):
一次安装所选用的全部包来源与版本快照，用于复现该次安装的源码状态。
_Avoid_: 锁文件, lockfile, 构建报告

**包目录** (package catalog):
已注册仓库与工程本地缓存中可独立浏览的包元数据集合，不依赖工程依赖图解析。
_Avoid_: 包索引, 全局仓库表

**项目模型** (resolved project model):
工程解析完成、尚未构建时一次性构造的项目视图，包含包、目标、配置、依赖边与诊断；构造完成后只读。
_Avoid_: 巡检, 快照, 检查结果, 构建计划

**包来源类型** (package source kind):
一个包源码的三分法——`local`（工程内）、`native`（原生仓库包）、`registry`（registry 仓库包）。
_Avoid_: 源类型字符串, origin, source type
