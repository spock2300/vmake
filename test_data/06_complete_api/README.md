# 06_complete_api - Complete API Coverage Test

This test case covers all untested APIs from the previous test cases.

## APIs Covered

### OptionType
- `OptionString` - String configuration option (`ssl_version`, `custom_prefix`)
- `OptionInt` - Integer configuration option (`thread_count`)

### Option Methods
- `SetShowIf()` - Conditional display (`ssl_version` only shows when `ssl=true`)

### ConfigContext
- `Bool()` - Read boolean config in OnConfig (used in `SetShowIf`)
- `Int()` - Read integer config in OnBuild (`thread_count`)
- `String()` - Read string config in OnBuild (`custom_prefix`, `c++standard`)

### TargetKind
- `TargetShared` - Shared library (conditionally built based on `shared_lib`)
- `TargetObject` - Object files (`core_obj`, `utils_obj`)

### Target Methods
- `SetLanguages()` - Set language standard (e.g., `c++17`)
- `AddCxxFlags()` - C++ compiler flags (`-Wall`, `-fPIC`, etc.)
- `AddLdFlags()` - Linker flags (`-Wl,-soname,libmylib.so`, `-Wl,--as-needed`)

### BuildContext
- `When()` - Conditional check (`ctx.When("shared_lib", true)`)
- `Bool()` - Boolean check in OnBuild (`ctx.Bool("verbose")`)

## Targets

| Target | Kind | Default | Description |
|--------|------|---------|-------------|
| `core_obj` | object | yes | Core object file |
| `utils_obj` | object | yes | Utils object file |
| `mylib` | static/shared | yes | Library (shared if `shared_lib=true`) |
| `myapp` | binary | yes | Main application |
| `benchmark` | binary | no | Benchmark tool (optimized) |
| `debug_info` | binary | no | Debug utility (only if `verbose=true`) |

## Configuration Options

| Option | Type | Default | Group | Description |
|--------|------|---------|-------|-------------|
| `debug` | bool | false | General | Enable debug mode |
| `verbose` | bool | false | General | Enable verbose output |
| `c++standard` | choice | c++17 | C++ | C++ standard version |
| `optimization` | choice | O2 | General | Optimization level |
| `ssl` | bool | false | SSL | Enable SSL support |
| `ssl_version` | string | 1.1.1 | SSL | SSL library version (shown only if ssl=true) |
| `thread_count` | int | 4 | Performance | Number of worker threads |
| `shared_lib` | bool | false | Build | Build as shared library |
| `custom_prefix` | string | /usr/local | Installation | Installation prefix |
