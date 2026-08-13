# 已知问题

## KI-001：5 个 `_test.go` 存在 UTF-8 损坏，阻塞 `go build` 与 CI

**发现**：2026-08-13，构建 Docker 镜像时
**状态**：未修复；已在 `.dockerignore` 中排除测试文件，使生产镜像可以构建
**影响**：`go build ./cmd/vohive`、`go vet`、`go test ./internal/api ./internal/device` 全部失败

### 症状

```
internal/api/card_policy_test.go:19:37: illegal UTF-8 encoding
```

Go 在加载包时会读取目录下所有 `.go` 文件（含 `_test.go`），任一文件存在非法 UTF-8
就会导致整个包无法编译——**即使只是注释里的字符损坏**。

### 范围

| 文件 | 损坏点 |
|------|--------|
| `internal/api/card_policy_test.go` | 35 |
| `internal/api/device_mgmt_phone_test.go` | 14 |
| `internal/api/proxy_test.go` | 11 |
| `internal/api/device_policy_display_test.go` | 4 |
| `internal/device/phone_number_sync_test.go` | 1 |
| **合计** | **65** |

其余 518 个 `.go` 文件均为合法 UTF-8。

> 用 `iconv -f UTF-8 -t UTF-8` 扫描会误报几百个文件，不可信。
> 可靠的检测方式：`new TextDecoder('utf-8', {fatal:true})`，见下方脚本。

### 损坏模式

一个三字节 UTF-8 字符的**第 3 字节，连同其后紧跟的那个字符**，被合并替换为单个 `?`(0x3F)：

```
正确： e7 94 a8  22        "用" + '"'
损坏： e7 94 3f            → 少了 1 字节，且引号消失
```

这是典型的 **UTF-8 被按 GBK 解码后再写回**：GBK 是双字节编码，`a8` 会去吞掉后面的
`22` 组成一个无效序列，最终落成一个 `?`。

因此每处损坏都丢了**两个字符**的信息（被毁的汉字 + 被吞的下一个字符），
无法从字节层面自动还原。

### 何时引入

该损坏在提交 `0868b32`（2026-08-11）时就已存在于仓库中，早于本轮前端工作。
在此之后仓库**从未成功构建过镜像**——当前运行的 `vohive:latest`（构建于
2026-08-11 09:08）早于该提交，日志显示它仍在用 SQLite。

### 为什么没有直接修

部分损坏点可以高置信还原，例如：

- `StateText: "未启【?】}` → `StateText: "未启用"}`（同文件有 `State: 0/1` 对照）
- `Operator = "中国联【?】` → `Operator = "中国联通"`

但另一些无法确定被吞掉的是什么字符：

- `t.Fatalf("应取卡策【?】 %+v", got)` —— 可能是 `策略: `、`策略，`、`策略 `
- `t.Fatalf("默认【?】sms=on/ip=v4: %+v", got)` —— `默认为 `？`默认值：`？

这些是原作者的断言文案。改错不会影响测试逻辑（都是失败时的提示串），
但会留下不准确的信息，因此交由熟悉原文的人确认。

由于 Go 要求整个文件合法，**必须 65 处全部修复**才能恢复编译，无法只修一部分。

### 检测脚本

```bash
node -e "
const fs=require('fs'),{execSync}=require('child_process');
for(const f of execSync('git ls-files \"*.go\"',{encoding:'utf8'}).trim().split('\n')){
  try{ new TextDecoder('utf-8',{fatal:true}).decode(fs.readFileSync(f)); }
  catch{ console.log('BAD:', f); }
}"
```

### 建议

1. 由熟悉这些测试的人逐处补回文案（65 处，多为重复的「已启用/未启用」）
2. 修复后从 `.dockerignore` 移除 `**/*_test.go`（或保留——生产镜像本就不需要测试代码）
3. 加一个 CI 前置检查，防止再次引入：把上面的检测脚本接进 `scripts/ci.sh`
