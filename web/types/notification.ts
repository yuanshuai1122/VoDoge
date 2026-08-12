/** 通知设置。对齐 internal/api/settings.go 的 notificationSettingsResponse。 */

export interface NotificationSettings {
  telegram: {
    enabled: boolean;
    bot_token: string;
    chat_id: number;
    admin_id: number;
    base_url: string;
    proxy: string;
  };
  feishu: {
    enabled: boolean;
    app_id: string;
    app_secret: string;
    chat_ids: string[];
  };
  qq: {
    enabled: boolean;
    app_id: string;
    app_secret: string;
    group_ids: string;
    direct_ids: string;
  };
  webhook: {
    enabled: boolean;
    urls: string[];
    secret: string;
    timeout_ms: number;
    retry_max: number;
    text_template: string;
    headers?: Record<string, string>;
  };
  weixin: {
    enabled: boolean;
    base_url: string;
    allowed_user_ids: string[];
  };
  bark: {
    enabled: boolean;
    urls: string[];
    group: string;
    icon: string;
    level: string;
  };
  email: {
    enabled: boolean;
    use_ssl: boolean;
    smtp_host: string;
    smtp_port: number;
    username: string;
    password: string;
    from_address: string;
    to_addresses: string[];
  };
  pushplus: {
    enabled: boolean;
    token: string;
    topic: string;
    channel: string;
  };
}

/** 后端只为这三个渠道提供测试接口。 */
export const TESTABLE_CHANNELS = ["webhook", "bark", "email"] as const;
