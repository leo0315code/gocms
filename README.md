# GoCMS · 通用企业官网内容管理系统

基于 **Go + MySQL** 的通用型 CMS，面向企业官网、产品站、品牌站、资讯站等场景。
单二进制部署，服务端渲染（SSR），SEO 友好，支持主题热切换、栏目自定义字段与可自定义的后台入口路径。

```
Go 1.22+ · MySQL 8.0+ · GORM · Tailwind CSS（后台）· Alpine.js · 单二进制
```

> 前台主题零构建；后台样式由 Tailwind 预编译，**产物已提交**，部署时不需要 Node。

---

## 一、快速开始

### 1. 环境要求

| 组件 | 版本 | 说明 |
| --- | --- | --- |
| Go | ≥ 1.22 | 需要 `net/http` 增强路由（`GET /a/{slug}`） |
| MySQL | ≥ 8.0 | InnoDB / utf8mb4，需 `FULLTEXT` 支持（InnoDB 内置） |
| Node.js | ≥ 18 | **仅改后台样式时需要**；日常编译部署不需要 |

### 2. 准备配置

```bash
cp .env.example .env
```

编辑 `.env`，至少改这几项：

```ini
DB_USER=root
DB_PASS=123456      # 你的 MySQL 密码
DB_NAME=gocms       # 库不存在会自动创建
APP_PORT=8090
ADMIN_PATH=admin    # 后台入口路径（仅首次生效，之后在后台「安全与入口」改）
ADMIN_USER=admin
ADMIN_PASS=Admin@123456
```

### 3. 启动

```bash
# 首次运行会自动：建库 → 建表 → 创建管理员 → 写入演示数据
go run ./cmd/gocms
```

或编译成单二进制：

```bash
go build -o gocms ./cmd/gocms
./gocms
```

- 前台：http://localhost:8090
- 后台：http://localhost:8090/admin

首次启动的演示内容包含：5 个栏目、8 篇文章、2 个单页、3 张轮播、3 条友链、9 个标签。

> **改管理员密码**：`.env` 里的 `ADMIN_PASS` 只在**首次创建管理员**时生效。
> 账号已存在后请到「后台 → 个人资料」修改，或直接改库。

---

## 二、目录结构

```
gocms/
├── cmd/gocms/main.go            # 入口：加载配置 → 迁移 → 种子 → 启动 HTTP
├── internal/
│   ├── config/                  # .env 解析 + DSN 拼装
│   ├── database/                # 连接池 + GORM 初始化 + go:embed SQL 迁移执行器
│   │   └── migrations/          # 0001_init.sql（16 张表）、0002_audit_logs.sql
│   ├── model/                   # 数据结构 + GORM 映射标签（gorm.go 为唯一权威定义）
│   ├── repository/              # 13 个仓储，GORM 实现（事务 / upsert / 批量关联）
│   ├── adminpath/               # 后台路径解析：校验、保留字、历史路径、原子热替换
│   ├── viewcount/                # 浏览量内存聚合：IP 去重 + 定时批量落库
│   ├── middleware/               # 含 ipguard.go（后台 IP 白名单守卫）
│   ├── security/                # bcrypt / HTML 净化 / Slugify / 安全重定向
│   ├── utils/                   # 分页、时间格式化、Markdown、通用小工具
│   ├── render/                  # 模板引擎（主题 + 后台，支持热重载）
│   ├── middleware/               # Recover/Logger/Headers/Gzip/AdminPath/Session/CSRF/限流
│   ├── handler/                 # 前台 + 后台控制器（audit.go 审计埋点，health.go 健康检查）
│   ├── router/                  # 路由表（dynamic.go 支持运行时重建）
│   └── seed/                    # 演示数据种子
├── themes/default/              # 前台主题（可复制成新主题）
│   ├── theme.json
│   ├── templates/               # 模板 + partials
│   └── assets/                  # CSS / JS / 图片
├── web/
│   ├── assets/src/input.css     # Tailwind 源文件（主题色板 + 组件层）
│   └── admin/
│       ├── templates/           # 后台模板（Tailwind 工具类 + Alpine）
│       └── static/              # app.css（构建产物）/ alpine.js / admin.js
├── scripts/audit-admin-ui.js    # 后台 UI 自动审计 + 截图（puppeteer-core）
├── tailwind.config.js           # 内容扫描 + 语义色板
├── uploads/                     # 上传文件（按 年/月 归档）
└── docs/
    ├── 01-设计文档.md
    ├── 02-实测验证记录.md
    ├── 03-部署指南.md
    ├── 04-重构方案.md            # GORM / 后台路径 / Tailwind 三项重构的依据与验收
    ├── screenshots/             # v1.0 截图存档
    └── screenshots-v2/          # 重构后截图存档
```

---

## 三、功能清单

### 内容管理

| 模块 | 能力 |
| --- | --- |
| 文章 | 富文本 / Markdown 双模式、封面、摘要、置顶、推荐、标签、定时发布、浏览量、上下篇、相关推荐、批量操作 |
| 栏目 | 无限级树形、类型（文章/单页/外链）、导航开关、模板选择、SEO、**自定义字段** |
| 单页 | 关于我们、联系方式等独立页面 |
| 标签 | 自动 slug、文章数统计、标签云 |
| 媒体库 | 拖拽上传、按年月归档、图片宽高与大小记录、图片选择器 |
| 菜单 | 多菜单（主导航/页脚），层级菜单项，支持分类/单页/自定义链接 |
| 轮播 | 多位置（首页/内页）、标题副标题按钮、排序 |
| 友情链接 | 名称、URL、排序 |
| 留言 | 前台表单（限流 + 蜜罐防垃圾）、后台已读/回复/删除 |

### 系统

- **用户与权限**：`admin`（全权）/ `editor`（内容编辑），后台菜单按角色自动收敛
- **设置中心**：基本信息 / SEO / 联系方式 / 显示 / 社交账号 / 安全与入口，分 Tab 管理
- **主题管理**：后台列出 `themes/` 下全部主题，一键切换（写库 + 热重载，无需重启）
- **操作日志**：登录、发文、改设置、删除等写操作自动落库，可按操作人/动作/关键字筛选，保留 180 天
- **SEO**：每页 title/keywords/description/canonical/OG/Twitter Card、`sitemap.xml`、`robots.txt`、`rss.xml`
- **统计**：后台概览卡片 + `/admin/stats.json`

### 前台主题自带

响应式布局、深浅色一键切换（localStorage）、粘性导航、下拉菜单、轮播、搜索、标签云、面包屑、分页、代码高亮样式、打印样式。

---

## 四、后台入口与默认账号

```
http://localhost:8090/admin      # 默认；可在后台改成任意前缀
```

**后台路径可自定义**，两种改法：

1. `.env` 里 `ADMIN_PATH=manage`（仅首次初始化生效）
2. 后台「系统设置 → 安全与入口 → 后台入口路径」——**改完立即生效，无需重启**

改完后旧地址如何处理，由「旧路径静默」开关决定：

| 开关 | 旧地址行为 | 适用场景 |
| --- | --- | --- |
| 关闭（默认） | 302 跳转到新地址 | 只想起个不撞车的名字，书签要能继续用 |
| 开启 | 返回与普通页面完全一致的 404 | **真的想隐藏后台**。关闭时 302 的 `Location` 头会把新地址告诉任何人 |

> ⚠️ 只改路径、不开静默，等于把新入口写在了 `/admin` 的跳转响应里——扫描器访问一次旧地址就拿到了。
> 另外 `robots.txt` 也做了对应处理：静默模式下**不会**列出后台路径（robots.txt 是公开文件，列出来等于广播）。

合法值：2~32 位字母/数字/下划线/连字符，且不能与 `a`、`c`、`p`、`t`、`assets`、`uploads` 等前台路径冲突。

### 后台 IP 白名单

「系统设置 → 安全与入口 → 后台 IP 白名单」，每行一条：

```
# 办公室固定 IP
203.0.113.10
# 公司网段
192.168.1.0/24

# 留空 = 不限制
```

- 只限制**后台**，前台永远可访问
- 支持单个 IP 与 CIDR 网段，`#` 开头为注释
- 保存时若新规则**不包含当前访问 IP 会直接拒绝**，避免把自己锁在门外
- 改完立即生效，不需要重启

> ⚠️ **反代部署必须配 `TRUST_PROXY=1`**。默认不信任 `X-Forwarded-For`，挂在 Nginx 后面时取到的是代理 IP，
> 白名单会全部拦掉。反过来，直连部署若设了 `TRUST_PROXY=1`，任何人都能伪造请求头绕过白名单。

### 登录保护

三个维度独立限流（5 次 / 15 分钟）：

| 维度 | 拦截的攻击形态 |
| --- | --- |
| 同 IP + 同账号 | 常规暴力破解 |
| 同账号（跨 IP） | 分布式撞库、代理池 |
| 同 IP（跨账号） | 用字典轮换用户名 |

登录成功即清零对应计数。失败提示统一为「用户名或密码错误」，不泄露账号是否存在。

| 项 | 值 |
| --- | --- |
| 用户名 | `admin` |
| 密码 | `.env` 中 `ADMIN_PASS`（默认 `Admin@123456`） |

---

## 五、常用命令

```bash
make help          # 列出所有命令
make run           # 开发运行（模板改动即时生效，无需重启）
make build         # 编译当前平台（macOS 自动补 ad-hoc 签名）
make build-linux   # 交叉编译 Linux amd64
make assets        # 改过后台模板/样式后重编 Tailwind
make assets-watch  # 监听模板改动自动重编
make check         # gofmt + go vet
make test          # 单元测试（仓储集成测试自动跳过）
make test-db       # 含仓储集成测试，需要 MySQL（测试库会被清空）
make test-cover    # 覆盖率报告
make start         # 后台启动（日志 .run/gocms.log）
make stop          # 停止
make sign          # macOS 报 "Code Signature Invalid" 时补签名
```

不用 make 也可以：

```bash
go run ./cmd/gocms
go build -o gocms ./cmd/gocms && codesign --force --sign - ./gocms   # macOS 建议签名
go vet ./...
go test ./...

# 仓储集成测试（会清空 gocms_test 库，别指向生产库）
GOCMS_TEST_DSN='root:123456@tcp(127.0.0.1:3306)/gocms_test?parseTime=true&charset=utf8mb4&loc=Local' \
  go test ./internal/repository/ -v

# 重编后台样式（仅改动 web/admin/templates 或 web/assets/src 后才需要）
npm install && npx tailwindcss -i ./web/assets/src/input.css -o ./web/admin/static/app.css --minify

# gofmt 不在 PATH 时用绝对路径
$(go env GOROOT)/bin/gofmt -l -w ./cmd ./internal
```

### 健康检查

```bash
curl -s http://127.0.0.1:8090/healthz
# {"status":"ok","db":"ok","uptime_seconds":1284,"db_latency_ms":1}
```

会真的 Ping 一次数据库，异常返回 `503`。不建会话、不受后台 IP 白名单限制，
可直接接 Nginx 上游判断或 cron 探活（见 [`docs/03-部署指南.md`](docs/03-部署指南.md) 4.1 节）。

## 六、部署要点

1. **必须改的三处**：`APP_ENV=production`、强随机 `ADMIN_PASS`、`APP_URL` 改成真实域名（影响 sitemap/RSS 绝对链接）。
2. **反代部署要设 `TRUST_PROXY=1`**，否则日志、限流、IP 白名单拿到的都是代理 IP。
   直连部署保持默认 `0`——否则伪造 `X-Forwarded-For` 就能绕过所有基于 IP 的保护。
3. `uploads/` 与数据库需持久化；`uploads/` 建议对 `.php` 等可执行后缀关闭解析。
4. 反代（Nginx）需要把 `/assets/`、`/uploads/`、后台静态资源前缀直出，其余转发到 Go 端口。
   后台静态资源路径随后台入口变化，如后台是 `/manage` 则需直出 `/manage/assets/`。
5. 伪静态 `/a/foo.html` 由程序内部重写，Nginx **不要**再做 `.html` 剥离，否则会 404。
6. 建议 systemd / supervisor 托管，并配 `ReadHeaderTimeout`（程序已内置 10s）。
7. 生产环境建议同时开启：后台路径改掉 + 旧路径静默 + IP 白名单（内网）。

示例 Nginx：

```nginx
server {
    listen 80;
    server_name example.com;
    client_max_body_size 12m;

    location ~ ^/(assets|uploads|admin/assets)/ {
        root /var/www/gocms;
        expires 7d;
        access_log off;
        try_files $uri =404;
    }

    location / {
        proxy_pass         http://127.0.0.1:8090;
        proxy_set_header   Host              $host;
        proxy_set_header   X-Real-IP         $remote_addr;
        proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
    }
}
```

---

## 七、换主题 / 做二次开发

主题就是一个目录，最少包含 `theme.json` + `templates/` + `assets/`：

```bash
cp -r themes/default themes/mytheme
# 改 theme.json 的 name / title
# 改 templates/ 里的 HTML 与 assets/css/style.css
```

然后在「后台 → 主题管理」切换即可（运行时热重载，不用重启）。

模板用 Go `html/template`，可用的自定义函数：

| 函数 | 用途 | 示例 |
| --- | --- | --- |
| `safeHTML` | 输出已净化的 HTML | `{{safeHTML .Post.Content}}` |
| `render` | 按 `content_format` 渲染 HTML/Markdown | `{{render .Post.Content}}` |
| `md` | 强制 Markdown 渲染 | `{{md .Page.Content}}` |
| `excerpt` | 截取纯文本摘要 | `{{excerpt .Post.Content 120}}` |
| `date` / `datetime` | 日期格式化 | `{{date .Post.PublishedAt}}` |
| `add` / `sub` / `seq` | 整数运算 | `{{add $i 1}}` |
| `default` | 空值兜底 | `{{default "了解详情" $b.ButtonText}}` |
| `eqs` / `hasPrefix` / `upper` / `lower` / `trim` / `size` / `bool` / `concat` / `firstChar` | 常用字符串与判断 | — |

**通用性的关键：栏目自定义字段。**
在「栏目 → 编辑 → 自定义字段」里给栏目加字段（文本/多行/下拉/数字/日期/图片），
该栏目下的文章编辑页会自动出现这些字段，前台用 `{{range .Fields}}` 渲染。
不用改代码就能把 CMS 变成「产品库」「房源列表」「案例库」等任意结构化内容站。

---

## 八、文档

| 文档 | 内容 |
| --- | --- |
| [`docs/01-设计文档.md`](docs/01-设计文档.md) | 架构分层、数据库 16 张表设计、路由表、安全设计、主题系统、验收清单、迭代路线 |
| [`docs/02-实测验证记录.md`](docs/02-实测验证记录.md) | 全量回归结果、已修复缺陷清单、界面截图索引、已知限制、复验命令 |
| [`docs/03-部署指南.md`](docs/03-部署指南.md) | 服务器初始化、systemd、Nginx、备份、升级、常见问题排查 |
| [`docs/04-重构方案.md`](docs/04-重构方案.md) | GORM 迁移、后台路径可配置、Tailwind 重构的依据、执行顺序与验收标准 |
| [`docs/screenshots/`](docs/screenshots/) | v1.0 截图存档（26 张） |
| [`docs/screenshots-v2/`](docs/screenshots-v2/) | 重构后截图存档（21 张后台 + 深浅色 + 移动端） |

## 九、技术选型说明

| 决策 | 原因 |
| --- | --- |
| **GORM** 作为数据访问层 | 常规 CRUD 由 ORM 承担，链式条件与事务可读性远胜手写 SQL；底层仍保留 `*sql.DB` 给复杂统计，共用同一连接池 |
| 迁移仍用纯 SQL（`go:embed`） | 不用 `AutoMigrate`，避免 ORM 擅自改动线上表结构；表结构变更全程可评审 |
| `html/template` SSR | SEO 天然友好，改动只需刷页面；不引入 React/Vue 降低交付复杂度 |
| 标准库 `net/http` 路由 | Go 1.22+ 已支持路径参数与方法匹配，无需第三方框架 |
| **后台路径运行时热替换** | `atomic.Value` 装载 `http.Handler`，改路径后重建路由表而非重写 URL，保证 handler 内看到的路径与用户一致 |
| **Tailwind CSS**（仅后台） | 用工具类替代 607 行手写 CSS；`@apply` 只保留必须有状态的组件（按钮/表单/表格/徽章） |
| **Alpine.js**（仅后台） | 下拉、抽屉、批量选择、编辑器 Tab 切换这类交互，用 15KB 声明式写法替代数百行 DOM 操作 |
| 语义色板走 CSS 变量 | `surface` / `ink` / `line` 三组变量承载深浅色，模板只写一次类名，不再重复 `dark:` 变体 |
| 服务端 Session 表 | 可主动踢下线、可查在线状态，比 JWT 更适合后台管理 |
| 主题目录化 | 换皮不改代码，客户交付时替换 `themes/` 即可 |
| 静态资源三种缓存策略 | uploads 内容不可变可一年，后台资源改了就生效，一刀切必然有一头出错 |
| 自建 /healthz 而非只看端口 | 端口在听不代表能干活的，DB 掉线时要让上游摘节点 |
| 浏览量内存聚合后批量落库 | 每次访问写一次库会把热文的行锁打满，且「刷新 +1」本身没有意义 |
