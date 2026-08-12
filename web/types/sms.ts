/**
 * 短信 DTO。对齐 internal/db 的 SMS / SMSContact，以及 internal/api 的
 * SMSWithDevice / SMSContactWithDevice（Go 嵌入结构在 JSON 中是扁平的）。
 */

/** SMS.type */
export const SMS_TYPE_INBOUND = 1;
export const SMS_TYPE_OUTBOUND = 2;

/** SMS.status */
export const SMS_STATUS_UNREAD = 0;
export const SMS_STATUS_READ = 1;
export const SMS_STATUS_SENT = 2;
export const SMS_STATUS_FAILED = 3;

export interface SMSMessage {
  id: number;
  imsi: string;
  iccid: string;
  peer: string;
  local_phone: string;
  sender: string;
  recipient: string;
  content: string;
  /** 1=接收，2=发送 */
  type: number;
  /** 0=未读，1=已读，2=发送成功，3=发送失败 */
  status: number;
  timestamp: string;
  created_at: string;
  /** 来自 SMSWithDevice */
  device_name: string;
}

export interface SMSContact {
  imsi: string;
  iccid: string;
  peer: string;
  last_sms_id: number;
  last_timestamp: string;
  last_content: string;
  last_type: number;
  unread_count: number;
  created_at: string;
  updated_at: string;
  /** 来自 SMSContactWithDevice */
  device_id: string;
  device_name: string;
  /** 本机号码，来自订阅信息 */
  local_phone: string;
}

export function isInbound(m: Pick<SMSMessage, "type">): boolean {
  return m.type === SMS_TYPE_INBOUND;
}

export function isSendFailed(m: Pick<SMSMessage, "status">): boolean {
  return m.status === SMS_STATUS_FAILED;
}
