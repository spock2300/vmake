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
│   ├── src/main.c
│   └── .vmake/config.json
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
└── 05_conditional/       # 条件表达式示例
    ├── build.go
    ├── src/main.c
    └── .vmake/config.json
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

## 预期行为

每个测试目录都应能执行以下命令：

```bash
cd test_data/01_simple_c
vmake config    # 应正确加载并显示配置界面
vmake build     # 应编译生成 hello 可执行文件
./hello         # 输出 "Hello, VMake!"
```
