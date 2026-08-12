# VoHive 数据库决策记录

### ADR-DB-001 仅 PostgreSQL

- **日期**：2026-08-11  
- **状态**：已确认  
- **决策**：切断 SQLite 运行时，生产与主路径仅使用 PostgreSQL。  
- **后果**：部署必须提供 PG；测试/CI 以 PG 为准；可选 `cmd/dbmigrate` 从旧 SQLite 文件一次性导入。  

### ADR-DB-002 时区与 SSL

- **日期**：2026-08-11  
- **决策**：UTC 存库；内网 compose `sslmode=disable`；公网另行 require。  
