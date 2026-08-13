/** 对齐 pkg/logger.LogEntry。SSE 的 `log` 事件与 /logs/history 共用此结构。 */
export interface LogEntry {
  time: string;
  level: string;
  caller: string;
  message: string;
  fields?: string;
}

/**
 * 后端按等级阈值过滤（entry >= filter），不是精确匹配。
 * 取值见 internal/api 的 matchLogLevel。
 */
export const LOG_LEVELS = ["debug", "info", "warn", "error"] as const;
export type LogLevel = (typeof LOG_LEVELS)[number];
