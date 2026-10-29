# 压测 Runbook

目标接口：`GET /:code`（核心读路径，命中 Redis 时不查 MySQL）。

## 0. 前置：安装 k6

```powershell
winget install k6
# 或 choco install k6
k6 version
```

## 1. 重新构建（配置有改动时）

```powershell
cd go-shorturl   # 仓库根目录
go build ./... ; go vet ./... ; go test ./...
```

## 2. 启动依赖

```powershell
docker compose -f deploy/docker-compose.yml up -d
docker ps    # 确认 mysql 和 redis 都是 Up，不是 Created/Exited
```

MySQL 首次启动要初始化数据库，等 healthcheck 变 healthy 再继续。

## 3. 以压测配置启动服务

**关键**：压测必须关掉 debug 模式和每请求访问日志，否则测的是日志 I/O 而不是服务性能。

```powershell
$env:GIN_MODE   = "release"   # 关掉 gin 的 debug 输出
$env:LOG_LEVEL  = "warn"      # 关掉每请求一条的 JSON 访问日志
$env:JWT_SECRET = "bench-secret"
go run ./cmd/server
```

生产化一点可以先 `go build -o bin/server.exe ./cmd/server` 再跑二进制，避免 `go run` 的编译开销混进测量。

## 4. 另开一个终端，准备测试数据

```powershell
$r = Invoke-RestMethod -Uri http://localhost:8080/api/v1/auth/register -Method Post `
  -ContentType 'application/json' `
  -Body '{"username":"bench","password":"bench123"}'
$H = @{ Authorization = "Bearer $($r.token)" }

$u = Invoke-RestMethod -Uri http://localhost:8080/api/v1/urls -Method Post `
  -Headers $H -ContentType 'application/json' `
  -Body '{"url":"https://go.dev/doc/effective_go"}'

$env:CODE = $u.code
echo "CODE = $env:CODE"

# 预热缓存，确保第一次压测不是在测冷启动回源
curl.exe -i "http://localhost:8080/$env:CODE"
```

## 5. 分档压测

```powershell
$env:VUS = "100";  k6 run scripts/load-test.js
$env:VUS = "500";  k6 run scripts/load-test.js
$env:VUS = "1000"; k6 run scripts/load-test.js
```

每档之间隔十几秒，让 flusher 把计数落库、连接池回落。

## 6. 记录资源占用

压测进行中，另开终端：

```powershell
# 服务进程的 CPU 与内存
Get-Process server, go | Select-Object Name, CPU, WorkingSet64

# 容器资源
docker stats --no-stream
```

## 7. 回填结果

把每档的 RPS / P50 / P95 / P99 / 错误率 / CPU / 内存填进 `README.md` 的「性能」小节。

**只填你实际测出来的数字。** 写进 README 的性能数据，必须能对应到本机真实跑过的一次压测。

---

## 结果解读

| 现象 | 可能原因 |
|---|---|
| RPS 上不去但 CPU 没满 | 连接池或 goroutine 阻塞，检查 `SetMaxOpenConns` |
| P99 远高于 P95 | 有长尾请求，通常是缓存未命中回源或 GC 停顿 |
| 错误率随并发上升 | 端口耗尽（Windows 动态端口范围有限）或连接池打满 |
| 并发提高但 RPS 持平 | 已达到瓶颈，看 `docker stats` 判断是 app 还是 Redis |

### Windows 上的注意事项

高并发短连接容易耗尽临时端口（默认范围约 16k 个，TIME_WAIT 回收要 2 分钟）。
若 1000 并发时出现大量连接错误，先确认是不是本机端口耗尽而不是服务本身的问题：

```powershell
netstat -an | Select-String "TIME_WAIT" | Measure-Object
```

k6 脚本已默认复用连接（未设 `noVUConnectionReuse: true`），可以缓解这个问题。

## 这个项目值得测的对比实验

单纯一个数字说服力有限，做成对比更能体现优化思路：

1. **缓存命中 vs 未命中** —— 压测前 `docker exec deploy-redis-1 redis-cli FLUSHALL`，对比首次回源与稳定命中的 P99 差异。
2. **singleflight 的作用** —— 清空缓存后瞬间打高并发，观察 MySQL 的 QPS（`SHOW GLOBAL STATUS LIKE 'Questions'` 前后差值）。理论上 N 个并发只产生 1 次查询。
3. **异步统计的收益** —— 与「每次跳转同步 UPDATE visit_count」的实现对比 RPS。
