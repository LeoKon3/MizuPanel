# MizuPanel 主机工作台 · 独立 UI 样板

用于设计验收，不替换正式页面。所有主机、地址、指标、进程和 AI 回复都是本地模拟数据。

## 启动

在仓库根目录执行：

```bash
cd web
NODE_ENV=development npx tsc --project prototype/tsconfig.json --noEmit
NODE_ENV=development npx vitest run --config vite.prototype.config.ts
NODE_ENV=production npx vite build --config vite.prototype.config.ts
cd ..
python3 -m http.server 8081 --bind 127.0.0.1 --directory bin/ui-prototype
```

访问 `http://127.0.0.1:8081/`，或 `http://localhost:8081/`。WSL mirrored 模式下从同一台 Windows 电脑使用这两个地址，不依赖宿主机网卡 IP 回环。

- 浅色列表：`/?theme=light#/hosts`
- 深色列表：`/?theme=dark#/hosts`
- 主机概览：`/#/hosts/node-a1/overview`
- 进程：`/#/hosts/node-a1/processes`
- AI 工作台：`/#/ai`

`theme` 查询参数显式指定主题；不传时沿用样板自己的 localStorage 设置。

## 已确认的设计

- 主机操作优先；服务 1–20 台主机的个人/小团队场景。
- 主机列表与详情分开，首页不自动选中一台主机。
- 列表突出名称/地址、状态、CPU、内存、磁盘及终端入口；默认按名称稳定排序，刷新不跳行。
- 详情提供可搜索的主机切换器，切换保持分区但重置目标数据。返回保留列表筛选和局部滚动位置。
- 概览承载趋势；进程、文件等工作区不再重复全局 KPI 卡片。
- 中等密度：正文 14px、单行表格 44px；主机名称+地址双行使用 48px，工具栏 32–34px。
- AI 按需打开覆盖式抽屉，不改变主工作区宽度。草稿与上下文在抽屉/工作台间保留，不自动发送。
- 深浅主题同结构；没有 emoji、装饰性眉题、渐变、发光、玻璃、卡片入场动画。

## Taste 方向与适配

`framed-sheet`，seed `528791084`，原始生成结果见 `direction.json`。

- 单一 L 形导航框架 + 一块有轻边界、无阴影的工作面；不增加第四层容器。
- Logo 保留项目自己的图片；低饱和冷灰框架与青蓝数据线呼应其颜色。
- 字体采用本地 IBM Plex Sans 400/500/600，中文 fallback 为系统 Noto Sans CJK SC / Microsoft YaHei / PingFang SC；ID、命令和编辑区使用等宽字体。
- 英文字体由 `@fontsource/ibm-plex-sans@5.2.8` 提取 Latin WOFF2；许可证在 `fonts/LICENSE`，没有 CDN 字体请求。
- 原方向仅提供浅色：根据用户确认增加同结构中性深色，不重新抽取另一种布局。
- 原列表样式行距更松：按用户确认的中等密度收紧；无模板的详情分区按实际主机任务组织。
- 桌面验收 1280×800、1440×900；包含基本窄屏兜底，但本轮不承诺完整移动端功能验收。

## 可体验范围

1. 搜索、分组、状态筛选、勾选与清除选择。
2. 主机概览和资源曲线切换；进程搜索与列排序。
3. 搜索切换主机，保留当前分区；返回列表保留筛选。
4. 本地文件草稿编辑，用于验证离开/切换主机及浏览器后退的未保存保护。
5. AI 抽屉、完整工作台、跨视图草稿、显式本地发送预览。
6. 页面底部切换正常、加载、空数据、请求失败，错误可重试。
7. 模拟离线、过期与零值。离线/过期指标为 `—`，真实零为 `0.0%`。
8. 终端为只读输出预览，没有输入命令的连接。

添加主机、分组管理、批量管理、容器/系统服务/日志/Agent 操作及其他全局页面只保留明确标注的范围入口，不宣称功能已接入。

## 隔离边界

- 独立 Vite root (`prototype/`)、配置 (`vite.prototype.config.ts`) 与产物 (`bin/ui-prototype/`)。
- 不导入正式 App 或 API client；仅复用 BrandLogo 和 Toast。
- HTML CSP `connect-src 'none'`，阻止 fetch/WebSocket；不使用后台、不保存业务数据、不调用模型。
- `192.0.2.0/24` 为文档示例地址，不能代表真实主机。
- 不修改正式入口、后端、版本、现有工作树改动或 `bin/web/`，`8080` 保持原样。
- 本轮是隔离设计样板，不是产品发布批次；正式接入前必须经用户确认并按发布规范处理版本。

## 验收原则

- Taste `lint --app` 及 `inspect --app`，对列表/概览/进程在两种主题、两种桌面宽度检查。
- 数据值与曲线末值来自同一数据源，状态统计由行数据计算；没有虚构线上统计。
- 留白告警可接受的边界：工作面保持视口高度，短进程列表不人为造行或拉伸行距填满；数据列表本身收缩，不在图表下强行钉住图例。
- Lint 的时钟补零、文件路径/代码等宽、对话框阴影及缺失值短横线是有语义的用途，不作为装饰。
- 后续正式实现必须重新接入真实数据、权限、保存和聊天确认路径，不能把样板的本地回复或模拟保存搬成业务实现。
