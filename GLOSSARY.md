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

**模块容器** (module container):
承载版本解析、信任校验、检出与容器级源码链接的仓库单元（本地工程根、远程 `native` 仓库或 `registry` 包装仓库）；本身不作为依赖图节点。
_Avoid_: 父包, 主包, 根仓, 依赖仓

**构建包** (package):
模块容器内由一份 `build.go` 定义的构建与依赖单元，以相对容器根的路径为规范身份（本地 `//<path>`、远程 `<repo>/<module>[/<path>]`）。
_Avoid_: 子包, 成员包, 模块

**独立上游源码** (independent upstream source):
构建包通过 `SetGit` 声明的独立第三方上游 Git 源码，与脚本所在模块容器正交地参与锁文件、安装清单与单包源码链接。
_Avoid_: 自带 git 子包, own-Git 子包, 第三方子包

**容器导出表** (container exports):
模块容器声明的对外公开逻辑身份与内部构建包路径的映射白名单；存在时未导出的构建包跨容器默认私有。
_Avoid_: 别名表, 快捷路径, 路由表

**开发根** (dev root):
模块容器仅在作为本地主工程构建或自测时生效的构建根；作为外部模块容器被消费时自动忽略。
_Avoid_: 测试包, 临时根, 伪入口

**内部选项** (internal option):
仅允许同模块容器内上层构建包驱动的配置项，不对外部采用者的配置空间与交互界面暴露。
_Avoid_: 隐藏开关, 私有宏, 底层参数
