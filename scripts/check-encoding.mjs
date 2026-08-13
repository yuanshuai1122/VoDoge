#!/usr/bin/env node
/**
 * 检查所有被追踪的 .go 文件是否为合法 UTF-8。
 *
 * Go 会因为包内任意一个文件含非法 UTF-8 而拒绝编译整个包，
 * 即使损坏只在注释里。仓库曾因此连续多次构建失败（docs/known-issues.md KI-001）。
 *
 * 注意：不要用 `iconv -f UTF-8 -t UTF-8` 做这件事，它在本仓库会误报数百个文件。
 * 严格解码器才是可靠的判据。
 *
 * 用法：
 *   node scripts/check-encoding.mjs          # 打印报告，有问题时退出码 1
 *   node scripts/check-encoding.mjs --list   # 只打印坏文件路径（供 shell 消费）
 */

import { readFileSync } from "node:fs";
import { execSync } from "node:child_process";

const listOnly = process.argv.includes("--list");

const files = execSync('git ls-files "*.go"', { encoding: "utf8" })
  .trim()
  .split("\n")
  .filter(Boolean);

const bad = [];
for (const f of files) {
  try {
    new TextDecoder("utf-8", { fatal: true }).decode(readFileSync(f));
  } catch {
    bad.push(f);
  }
}

if (listOnly) {
  process.stdout.write(bad.join("\n"));
  process.exit(0);
}

if (bad.length === 0) {
  console.log(`encoding ok — ${files.length} Go files are valid UTF-8`);
  process.exit(0);
}

console.error(`${bad.length} Go file(s) contain invalid UTF-8:`);
for (const f of bad) console.error(`  ${f}`);
process.exitCode = 1;
