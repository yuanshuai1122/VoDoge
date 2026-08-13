# 已知问题

## KI-001：5 个 `_test.go` 存在 UTF-8 损坏 —— ✅ 已修复（2026-08-13）

**发现**：2026-08-13，构建 Docker 镜像时
**修复**：2026-08-13，65 处全部还原
**影响过**：`go build ./cmd/vohive`、`go vet`、`go test ./internal/api ./internal/device` 全部失败

### 曾经的症状

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

### 损坏模式

损坏工具是**按行处理**的（行尾 CRLF 完好），因此分两种情况：

```
行中： e7 94 a8 22   →  e7 94 3f      "用" + '"'  被合并吞成一个 '?'
行尾： ef bc 8c      →  ef bc 3f      仅第 3 字节被替换，其后的 CRLF 未受影响
```

即：三字节 UTF-8 字符的**第 3 字节**被替换为 `?`(0x3F)；若该字符不在行尾，
**其后的那一个字节也被一并吞掉**。这是 UTF-8 被按 GBK 解码后再写回的典型结果——
GBK 是双字节编码，落单的高位字节会去吞掉后面一个字节组成无效序列，最终落成一个 `?`。

### 如何还原的

被吞的字符无法从字节层面自动恢复，全部依据上下文推断，多数有强约束：

| 依据 | 例子 |
|------|------|
| 字符串必须闭合 | `StateText: "未启【?】}` → 被吞的只能是 `"`，还原 `"未启用"}` |
| 括号必须配对 | `(如测试环【?】,` → 被吞的是 `)`，还原 `(如测试环境),` |
| 同文件的风格一致 | `proxy_test.go` 用半角逗号，故还原为 `里,不从文件读取` |
| 上下文对仗 | 开 VoWiFi「只置 vowifi」↔ 关 VoWiFi「只清 vowifi」 |
| 同一序列的不同还原 | `验证设备**无** ICCID **时** applied=false`（两处 `e6 97 3f` 语义不同，按前后文分别还原） |

少数断言文案（如 `t.Fatalf("payload 错: %+v"`）中被吞的分隔符按同文件其它断言的
`描述: %+v` 格式统一还原为 `:`，属推断，不影响测试逻辑（仅失败时的提示串）。

### 验证

- 全库 523 个 `.go` 文件均为合法 UTF-8
- 5 个还原文件 + 2 个新增测试文件均通过 `gofmt -e` 语法检查
- `go vet ./internal/api/... ./internal/device/... ./internal/db/...` 通过

### 防止再次引入

`scripts/ci.sh` 的 `hygiene` 环节已加入编码检查：

```bash
node -e "
const fs=require('fs'),{execSync}=require('child_process');
for(const f of execSync('git ls-files \"*.go\"',{encoding:'utf8'}).trim().split('\n')){
  try{ new TextDecoder('utf-8',{fatal:true}).decode(fs.readFileSync(f)); }
  catch{ console.log('BAD:', f); }
}"
```

> 注意：`iconv -f UTF-8 -t UTF-8` 会误报几百个文件，不要用它做这项检查。

### 遗留

`.dockerignore` 中的 `**/*_test.go` 排除**保留**——生产镜像本就不需要测试代码，
且能减小构建上下文。它不再是绕过损坏的手段。
