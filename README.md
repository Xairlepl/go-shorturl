# go-shorturl

Go + Gin + MySQL + Redis 实现的短链接服务：注册登录、生成短链、302 跳转、访问统计。

设计的重心在**读路径**——跳转是高频操作，必须尽量不碰 MySQL。所以做了 Redis 缓存、singleflight 合并回源、空值缓存防穿透，以及把访问计数从写路径上摘下来异步落库。

本机 k6 实测：**100 并发下 7,631 RPS，P95 22.32 ms，错误率 0**（服务、MySQL、Redis、k6 跑在同一台机器上，详见下方测试环境）。

## 功能

- 用户注册 / 登录，密码用 bcrypt 存储，签发 JWT
- 创建短链接：6 位 Base62 随机码（`crypto/rand`，不可预测）
- 我的链接：分页列表、删除、单项访问统计
- `GET /:code` 302 跳转，Redis 命中时不访问 MySQL
- 缓存未命中时用 `singleflight` 合并并发回源，只查一次库
- 访问计数走 Redis INCR，定时批量落库；访问明细异步写入，不阻塞跳转
- Docker Compose 一键拉起 MySQL + Redis，`/healthz` 健康检查，信号驱动的优雅退出

## 性能

### 测试环境

| 项 | 配置 |
|---|---|
| CPU | 12th Gen Intel Core i5-12450H（8 核 12 线程，移动版） |
| 内存 | 16 GB |
| 操作系统 | Windows 11 家庭版 |
| 服务 | `go run` 或本地二进制，同机运行 |
| 依赖 | MySQL 8.4 / Redis 7 通过 Docker Compose 启动，同机 |
| 压测客户端 | k6，同样跑在这台机器上 |

**这组数字要带着这个前提读**：客户端、服务、MySQL、Redis 四者抢同一个 CPU 和同一块网卡。它证明的是「单机全栈能跑到什么程度」，不代表服务端在独立机器上的上限。

启动参数：`GIN_MODE=release`、`LOG_LEVEL=warn`（关掉每请求访问日志，否则测的是 stdout I/O）。

### 结果

基线对照——无效短码，请求在 `shortcode.IsValid` 阶段就被拒绝，完全不触达 Redis 和 MySQL，代表框架层天花板：**15,474 RPS / P95 9.93 ms**。

缓存命中路径 `GET /:code`（302 跳转），每组压测 30 秒：

| 并发 | 请求数 | RPS | P50 (ms) | P95 (ms) | P99 (ms) | max (ms) | 错误率 |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 100 | 229,198 | 7,631.29 | 11.82 | 22.32 | 33.87 | 226.90 | 0.00% |
| 500 | 224,661 | 7,475.57 | 64.42 | 88.49 | 111.01 | 468.77 | 0.16% |
| 1000 | 215,431 | 7,158.34 | 135.30 | 168.96 | 190.33 | 553.11 | 0.37% |

### 怎么读这组数字

- **100 并发是拐点。** 再往上加并发，RPS 反而略降，延迟和错误率明显上升——瓶颈已经不在应用逻辑，而在单机的连接与调度上。
- **缓存命中路径约为框架天花板的 49%。** 差额来自 Redis 往返，以及跳转路径上的计数 INCR。
- **错误率上升不是业务错误。** 500 并发起出现的 0.16% / 0.37%，典型来源是 Windows 动态端口范围和连接池排队，不是跳转逻辑出错。定位这类问题应该看 `docker stats` 判断是应用、MySQL 还是 Redis 先到顶。

完整复现步骤见 [docs/BENCHMARK.md](docs/BENCHMARK.md)。

## 快速开始

```bash
# 1. 启动 MySQL 与 Redis
docker compose -f deploy/docker-compose.yml up -d

# 2. 拉依赖
go mod tidy

# 3. 配置环境变量
cp .env.example .env    # 按需修改；不配置时全部使用默认值

# 4. 运行（首次启动会 AutoMigrate 建表）
go run ./cmd/server
```

MySQL 容器映射到宿主机 **3307**，避开本机常见的 3306 占用。换端口用 `MYSQL_PORT=3308 docker compose ...`，并同步修改 `MYSQL_DSN`。

生产环境可以用 `migrations/001_init.sql` 显式建表，不依赖 AutoMigrate。

### 接口示例

```bash
# 注册，拿到 token
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"secret123"}'

TOKEN=<上一步返回的 token>

# 创建短链接
curl -X POST http://localhost:8080/api/v1/urls \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://go.dev/doc/effective_go"}'

# 列表 / 统计 / 删除
curl -H "Authorization: Bearer $TOKEN" 'http://localhost:8080/api/v1/urls?page=1&page_size=20'
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/urls/1/stats
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/urls/1

# 跳转（-i 看 302 与 Location）
curl -i http://localhost:8080/<code>
```

PowerShell 里 `curl` 是 `Invoke-WebRequest` 的别名，参数不通用，改用 `Invoke-RestMethod`；只有看 302 响应头时需要显式调 `curl.exe`。

## API

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/healthz` | 否 | 健康检查 |
| POST | `/api/v1/auth/register` | 否 | 注册，返回 JWT |
| POST | `/api/v1/auth/login` | 否 | 登录，返回 JWT |
| POST | `/api/v1/urls` | 是 | 创建短链接 |
| GET | `/api/v1/urls` | 是 | 我的链接，分页 |
| DELETE | `/api/v1/urls/:id` | 是 | 删除 |
| GET | `/api/v1/urls/:id/stats` | 是 | 访问统计 |
| GET | `/:code` | 否 | 302 跳转 |

## 设计要点

### 读路径：Redis → MySQL 回源

```
GET /:code
    │
    ├─ Redis HIT ────────────────────► 302
    │
    └─ MISS ─► singleflight ─► MySQL ─► 回填缓存 ─► 302
```

缓存时间走 `CACHE_TTL`。数据库里查不到时写一个空值标记（`EMPTY_CACHE_TTL`，默认 1 分钟），避免不存在的短码反复打到 MySQL。

### 防击穿：singleflight 合并回源

同一个 `code` 的并发未命中会用 `singleflight.Group` 合并成一次查询，其余请求等结果。注意它只在**单进程内**生效——多实例部署时每个实例仍会各自回源一次，要彻底解决得用分布式锁或逻辑过期。

### 写路径：计数与明细都离开跳转链路

跳转时只做一次 Redis INCR，立刻返回 302。后台有两个 goroutine：

- **flusher**：按 `VISIT_FLUSH_INTERVAL`（默认 10 秒）把 Redis 里的增量批量写回 `short_urls.visit_count`
- **detail**：消费内存缓冲区，把访问明细批量写入 `url_visits`

明细写入时维护了一个 `code → url_id` 的内存缓存，避免每条明细都查一次库。

进程正常退出时，flusher 会在关闭阶段再 flush 一次，避免最后一个周期的计数丢失。

### 失败处理

- **缓存写失败不影响主流程。** 创建时回填缓存、删除时失效缓存，失败只记 warn 日志，靠后续读取重建。
- **缓存读失败降级到 MySQL。** 不因为 Redis 抖动让跳转整体失败。
- **并发创建短码冲突靠唯一索引兜底。** `short_code` 上有唯一索引，冲突时重试。

### 安全

- 密码只存 bcrypt 哈希（`golang.org/x/crypto/bcrypt`）
- JWT Secret 从环境变量读；启动时如果还是默认的 `change-me` 会告警
- 原始 URL 只接受 `http` / `https` 协议，防 `javascript:` 之类的重定向滥用
- 短码用 `crypto/rand` 生成，不用自增转码，避免被顺序遍历
- `/api/v1/urls/*` 全部经过 JWT 中间件，并且按 `user_id` 做归属校验

## 目录结构

```text
cmd/server/          程序入口：装配依赖、启动 HTTP 服务、优雅退出
internal/config/     环境变量加载与默认值
internal/handler/    HTTP 层：参数绑定、响应
internal/service/    业务逻辑：短链、缓存、统计 worker
internal/repository/ 数据访问（GORM）
internal/model/      数据模型
internal/middleware/ RequestID、日志、Recovery、CORS、JWT
internal/cache/      Redis 封装
pkg/shortcode/       Base62 短码生成与校验
pkg/jwtutil/         JWT 签发与解析
migrations/          建表 SQL
deploy/              docker-compose.yml
scripts/             k6 压测脚本
docs/                架构说明、压测 runbook
```

## 测试

```bash
go test ./...
```

覆盖了短码生成与校验、JWT 签发解析、Service 层缓存分支、Router 层接口契约。

## 压测

```bash
CODE=<真实短码> VUS=100 k6 run scripts/load-test.js
```

完整步骤、记录方式和结果解读见 [docs/BENCHMARK.md](docs/BENCHMARK.md)。

## 已知限制

这些是明确没做、而不是不知道的部分：

- **统计是最终一致。** 计数按周期批量落库，非正常退出（进程被杀、断电）会丢掉最后一批未落库的增量。
- **singleflight 只在单进程内合并。** 多实例部署时防击穿能力会退化。
- **缓存与数据库没有强一致保证。** 删除时先删库再删缓存，缓存删除失败靠 TTL 兜底，中间存在短暂的不一致窗口。
- **短码冲突是重试式解决**，不是预分配。极端并发创建时会有重试开销。
- **没有做限流和防刷。** 创建接口和跳转接口都没有速率限制。
