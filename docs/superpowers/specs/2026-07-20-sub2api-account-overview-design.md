# Sub2API 智能调度聚合页面设计

## 目标

在 UpstreamOps 中提供独立的 Sub2API 智能调度聚合页面。页面使用唯一已配置目标的 Admin API Key，按 Sub2API 分组展示主调度池和保底池，不把 OAuth 授权账号误当作上游账号逐条展示。

页面允许管理员修改真实上游账号的 `schedulable`，以及修改真实上游和虚拟池的智能调度权重。不修改 Sub2API 源码。

## 数据口径

分组路由读取 Sub2API `model_routing`：

- `*`：主调度池账号或虚拟池 ID。
- `__fallback__`：保底池账号或虚拟池 ID。
- `__weights_primary__`：主调度池权重。
- `__weights_fallback__`：保底池权重。

正 ID 表示真实上游账号，展示账号名称、平台、并发、代理、托管状态和调度开关。负 ID 表示 OAuth 虚拟池，只展示池名称和当前可用授权账号数量，不下发授权账号明细。

支持以下虚拟池：

- `-10001`：OpenAI OAuth Plus。
- `-10002`：OpenAI OAuth Pro。
- `-10003`：OpenAI OAuth K12。
- `-10004`：OpenAI OAuth Team。
- `-10005`：OpenAI OAuth Free。

虚拟池成员按 Sub2API 当前可调度规则计数：账号必须为启用且可调度状态，并排除已过期、限流、过载或临时不可调度的账号。

## 页面结构

页面路由为 `/sub2api-overview`，包含：

1. 目标名称、地址和刷新操作。
2. 分组、智能调度、真实上游和虚拟池摘要。
3. 搜索、平台及智能调度状态筛选。
4. 横向可滚动的分组 Tab，一次只渲染一个分组面板。
5. 当前分组的主调度池和保底池。

每个池条目提供 `1-999` 整数权重输入。当前分组发生改动后才启用“保存权重”按钮。真实上游继续提供独立调度开关，虚拟池不提供账号级开关。

## 后端接口

### 聚合查询

```text
GET /api/upstream-sync/overview
```

服务端读取唯一目标、分组、全量账号和代理，关联本地托管映射，返回显式脱敏 DTO。响应禁止包含 Admin API Key、账号 `credentials`、授权账号明细及代理认证信息。

### 账号调度

```text
PUT /api/upstream-sync/accounts/:account_id/schedulable
```

仅接受正整数真实账号 ID 和布尔 `schedulable`，不修改账号 `status`。

### 调度权重

```text
PUT /api/upstream-sync/groups/:group_id/smart-routing
```

请求同时提交当前主池和保底池的 ID、权重。服务端重新读取远端分组，验证 ID 数量、顺序与当前路由完全一致，只替换两个权重键。模型专用路由、池成员、`model_routing_enabled` 和分组其他配置全部保留。

该约束防止过期页面或篡改请求借权重接口增删、重排账号池。池结构变化时要求刷新后重试。

## 错误处理

- 没有目标或存在多个目标时返回配置冲突，页面显示明确空状态。
- 刷新失败时保留上一次成功数据并显示错误。
- 权重不是 `1-999` 整数时由前后端同时拒绝。
- 账号池结构已变化时拒绝保存，避免覆盖新配置。
- 调度或权重请求进行期间锁定对应控件，避免重复提交。

## 安全边界

- Admin API Key 只在服务端解密和使用。
- 浏览器不直接请求 Sub2API。
- 聚合 DTO 不序列化远端账号凭据。
- 权重接口只发送 `model_routing` 和 `model_routing_enabled` 两个远端字段。
- 不修改 Sub2API 源码、数据库和部署配置。

## 验证范围

- 全量 Go 测试。
- 前端 ESLint、TypeScript 检查和生产构建。
- 桌面与移动端检查 Tab、权重输入和页面宽度。
- 硬编码密钥、Token、密码和私钥特征扫描。
