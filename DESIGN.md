# LLM Exporter 设计文档

## 1. 背景与目标

### 1.1 问题

大语言模型（LLM）API 服务的可用性和性能直接影响业务稳定性。与传统 HTTP 服务不同，LLM API 有以下特殊性：

- **响应时间长且不确定** — 从数百毫秒到数十秒不等，且受排队、推理负载影响显著
- **首 Token 延迟（TTFT）是核心体验指标** — 用户感知的"等待时间"取决于第一个 token 何时到达，而非整个请求完成
- **多供应商、多模型并存** — 生产环境通常同时使用多个 LLM 供应商，需要统一监控
- **Token 消耗即成本** — 每次调用的 token 数直接关联费用

现有监控工具（如 blackbox-exporter）无法满足这些需求：它们不理解 SSE 流式协议，无法测量 TTFT，也无法解析 token 用量。

### 1.2 目标

构建一个 Prometheus Exporter，提供：

1. **端到端可用性探测** — 定期向 LLM API 发起真实的流式请求，验证服务是否正常响应
2. **分阶段延迟测量** — 分别测量连接建立（DNS+TCP+TLS）、首 Token 延迟（TTFT）、完整请求时长
3. **Token 消耗与生成速率** — 采集每次探测的输入/输出 token 数和生成速率
4. **多供应商统一** — 支持 OpenAI、Anthropic、Google Gemini、Azure OpenAI 及所有 OpenAI 兼容服务（阿里百炼、字节方舟、DeepSeek、Mistral、OpenRouter、自定义代理等）
5. **最小外部依赖** — 不引入任何 LLM SDK，仅使用 HTTP + SSE 协议直接交互，3 个直接依赖

### 1.3 非目标

- 不替代 APM 系统（如对业务请求的全链路追踪）
- 不做请求代理或网关
- 不评估模型输出质量（只关心"能不能正常返回"和"多快返回"）
- 不管理 API Key 轮换或费用控制

## 2. 架构设计

### 2.1 整体架构

```
┌──────────────────────────────────────────────────────────────────┐
│                        LLM Exporter                              │
│                                                                  │
│  ┌─────────┐    ┌───────────────────────────────────────────┐    │
│  │  main   │    │              Scheduler                     │    │
│  │         │    │                                             │    │
│  │ HTTP    │    │  ┌──────────┐  ┌──────────┐  ┌──────────┐ │    │
│  │ Server  │    │  │ Target 1 │  │ Target 2 │  │ Target N │ │    │
│  │         │    │  │goroutine │  │goroutine │  │goroutine │ │    │
│  │/metrics │    │  │          │  │          │  │          │ │    │
│  │/healthz │    │  │ ticker   │  │ ticker   │  │ ticker   │ │    │
│  │/-/reload│    │  │   ↓      │  │   ↓      │  │   ↓      │ │    │
│  └────┬────┘    │  │ Prober   │  │ Prober   │  │ Prober   │ │    │
│       │         │  └────┬─────┘  └────┬─────┘  └────┬─────┘ │    │
│       │         └───────┼─────────────┼─────────────┼───────┘    │
│       │    ┌────────┐   │             │             │             │
│       │    │  File  │   │  reload()   │             │             │
│       │    │Watcher ├───┤◄────────────┤             │             │
│       │    │(可选)  │   │  SIGHUP     │             │             │
│       │    └────────┘   │             │             │             │
│  ┌────▼─────────────────▼─────────────▼─────────────▼────────┐   │
│  │                  Prometheus Registry                       │   │
│  │  ProbeSuccess | ProbeDuration | ProbeTTFT | ProbeTokens   │   │
│  │  ProbeConnectDuration | ProbeTokenRate | ProbeErrors       │   │
│  │  ProbeLastSuccess                                          │   │
│  └───────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
         │                    │              │
         ▼                    ▼              ▼
   ┌──────────┐        ┌──────────┐   ┌──────────┐
   │Prometheus│        │ OpenAI   │   │Anthropic │  ...
   │  Server  │        │   API    │   │   API    │
   └──────────┘        └──────────┘   └──────────┘
```

### 2.2 主动调度 vs 被动拨测

**设计决策**：采用主动调度（Active Scheduling），而非 blackbox-exporter 的被动拨测（On-Scrape）模式。

**原因**：

| 维度 | 被动拨测（blackbox-exporter） | 主动调度（本项目） |
|------|------------------------------|-------------------|
| 触发方式 | Prometheus 每次 scrape 触发探测 | 内部 ticker 独立触发 |
| 超时限制 | 受 `scrape_timeout` 约束（通常 10-15s） | 每个 target 独立 timeout（可设 60s+） |
| 适用场景 | 快速响应的 HTTP/TCP/DNS 检查 | LLM API 响应时间 3-60s |
| 指标时效 | 实时（每次 scrape 重新探测） | 近实时（最新探测结果缓存在 Gauge/Counter 中） |

LLM API 的响应时间通常在 3-30 秒，大幅超过 Prometheus 默认的 scrape_timeout。主动调度让探测与采集解耦，Prometheus 只需读取已有的指标值，不会因为 scrape 超时丢失数据。

### 2.3 模块划分

```
llm-exporter/
├── cmd/llm-exporter/main.go          # 入口：配置加载、HTTP server、信号处理、文件监听
├── internal/
│   ├── config/config.go              # YAML 配置解析 + 环境变量展开
│   ├── metrics/metrics.go            # Prometheus 指标定义与注册
│   ├── prober/
│   │   ├── prober.go                 # Prober 接口、工厂函数、HTTP 客户端、连接追踪
│   │   ├── sse.go                    # SSE 事件流解析器
│   │   ├── errors.go                 # 错误分类
│   │   ├── openai.go                 # OpenAI 兼容协议探测（流式/非流式）
│   │   ├── anthropic.go              # Anthropic 协议探测
│   │   ├── google.go                 # Google Gemini 协议探测
│   │   └── azure.go                  # Azure OpenAI 协议探测（流式/非流式）
│   ├── scheduler/scheduler.go        # 目标调度、指标更新、热重载、告警、响应验证
│   └── version/version.go            # 版本信息（通过 ldflags 注入）
```

**依赖关系**：

```
main → config, metrics, scheduler, fsnotify
scheduler → config, metrics, prober
prober → config (Target 配置)
metrics → prometheus/client_golang（无内部依赖）
```

## 3. 核心设计

### 3.1 探测流程

每次探测执行以下步骤：

```
1. 构建请求
   ├── 根据 api_format 选择请求格式（OpenAI/Anthropic/Google）
   ├── 追加时间戳后缀防止缓存: "prompt [t=1710000000000]"
   └── 设置 stream: true

2. 建立连接（通过 httptrace 计时）
   ├── DNS 解析
   ├── TCP 握手
   └── TLS 握手
   → 记录 ConnectDuration

3. 发送请求，开始接收 SSE 流
   ├── 等待第一个包含内容的事件
   │   → 记录 TTFT
   ├── 持续消费流，解析 token 用量
   └── 流结束（[DONE] / message_stop / EOF）
   → 记录 Duration

4. 计算派生指标
   ├── generation_time = Duration - TTFT
   ├── token_rate = OutputTokens / generation_time（需满足阈值）
   └── Success = TTFT 是否被记录

5. 更新 Prometheus 指标
```

### 3.2 时间分解模型

一次完整的 LLM API 请求可分解为三个阶段：

```
├─── ConnectDuration ───┤
│  DNS + TCP + TLS      │
│                       ├───── Wait ─────┤
│                       │  排队 + 预处理  │
│                       │                ├────── Generation ──────┤
│                       │                │  token 逐个生成         │
├───────────────────────┴────────────────┴────────────────────────┤
│                         Duration (总时长)                        │
│                                                                 │
0                     ConnectDone        TTFT                   Done

Wait = TTFT - ConnectDuration    （服务端排队 + prompt 处理时间）
Generation = Duration - TTFT      （token 生成时间）
TokenRate = OutputTokens / Generation
```

这个三阶段模型让运维团队可以快速定位瓶颈：
- ConnectDuration 高 → 网络/DNS 问题
- Wait 高（TTFT - Connect） → 服务端排队或 prompt 过长
- Generation 高 → 模型推理慢或 GPU 负载高

### 3.3 SSE 流式协议处理

三种 API 格式的 SSE 处理差异：

| 维度 | OpenAI | Anthropic | Google Gemini | Azure OpenAI |
|------|--------|-----------|---------------|--------------|
| 请求路径 | `POST /v1/chat/completions` | `POST /v1/messages` | `POST /v1beta/models/{model}:streamGenerateContent?alt=sse` | `POST /openai/deployments/{model}/chat/completions?api-version=...` |
| 启用流式 | `"stream": true` | `"stream": true` | URL 参数 `alt=sse` | `"stream": true` |
| 流结束标志 | `data: [DONE]` | `event: message_stop` | `io.EOF` | `data: [DONE]` |
| TTFT 判定 | `choices[0].delta.content != ""` | `event: content_block_delta` + `delta.type == "text_delta"` | `candidates[0].content.parts[0].text != ""` | 同 OpenAI |
| Token 用量 | 最后一个 chunk 的 `usage` 字段（需 `stream_options.include_usage`） | `message_start` 含 input_tokens，`message_delta` 含 output_tokens | `usageMetadata` 字段 | 同 OpenAI |
| 认证方式 | `Authorization: Bearer <key>` | `x-api-key: <key>` + `anthropic-version` | URL 参数 `key=<key>` | `api-key: <key>` |

SSE 解析器（`sse.go`）是共享组件，处理 `event:` 和 `data:` 字段的解析，支持多行 data。各协议探测器只需处理自己的 JSON 结构。

### 3.4 连接计时实现

使用 `net/http/httptrace` 包精确测量连接阶段：

```go
trace := &httptrace.ClientTrace{
    DNSStart:         → 记录 DNS 开始时间
    ConnectStart:     → 记录 TCP 连接开始时间
    ConnectDone:      → 记录 TCP 连接完成时间
    TLSHandshakeDone: → 记录 TLS 完成时间
    GotConn:          → 记录连接获取时间 + 是否复用
}

ConnectDuration = max(TLSHandshakeDone, ConnectDone, GotConn) - min(DNSStart, ConnectStart)
```

为确保每次探测都建立新连接，HTTP 客户端设置 `DisableKeepAlives: true`，禁止连接复用。

### 3.5 错误分类

探测失败按以下规则分类，写入 `error_type` label：

| 错误来源 | error_type | 判定规则 |
|---------|------------|---------|
| HTTP 401/403 | `auth` | 认证失败 |
| HTTP 429 | `rate_limit` | 触发限流 |
| 其他 HTTP 错误 | `api_error` | 服务端返回非 200 |
| 连接超时 / context deadline | `timeout` | 包含 `net.Error.Timeout()` 或 context deadline exceeded |
| 请求被取消 | `canceled` | context.Canceled（热重载或优雅关闭期间） |
| DNS/TCP/TLS 失败 | `network` | 其他网络层错误 |
| 流解析失败 / 无内容 | `parse_error` | SSE 解析异常或流中无 content |
| 响应内容不匹配 | `validation_error` | 配置了 `expect_pattern` 但响应未匹配 |

### 3.6 Token Rate 计算

Token 生成速率需要避免小样本导致的异常值：

```go
genDuration := result.Duration - result.TTFT

if genDuration >= 100ms && result.OutputTokens >= 5 {
    rate = float64(OutputTokens) / genDuration.Seconds()
}
```

**阈值设计**：
- `MinTokenRateOutputTokens = 5` — 少于 5 个 token 的输出不具有统计意义
- `MinTokenRateGenDuration = 100ms` — 小于 100ms 的生成时间，除法结果会被极度放大

不满足阈值时不更新 `ProbeTokenRate` 指标，保留上一次有效值。

## 4. 探测方法论

为获得科学、可复现的测量数据，探测设计遵循以下原则：

### 4.1 禁用连接复用

```go
Transport: &http.Transport{
    DisableKeepAlives: true,
}
```

每次探测建立全新的 TCP 连接。如果复用连接，ConnectDuration 会始终为 0，无法反映真实的网络状况。

### 4.2 Prompt 随机化

```go
func probePrompt(base string) string {
    return fmt.Sprintf("%s [t=%d]", base, time.Now().UnixMilli())
}
```

在 prompt 末尾追加毫秒级时间戳，确保：
- 代理/CDN 不会命中缓存
- 供应商的 prompt caching 机制不会影响测量
- 每次请求都经历完整的推理过程

### 4.3 可配置的输出量

默认 prompt 为 `"Hi"`，`max_tokens = 20`，以最小化 API 成本。对需要精确 token rate 数据的场景，可调大 `max_tokens` 或使用自定义 prompt 来产生更多 token。

### 4.4 独立探测间隔

每个 target 独立的 goroutine + `time.Timer`，互不影响。某个 target 的超时或高延迟不会阻塞其他 target 的探测。

启用自适应间隔（`adaptive_interval: true`）后，探测间隔在服务稳定时自动翻倍增长（封顶于 `max_interval`），一旦检测到失败立即恢复到基础间隔，在不影响故障检测灵敏度的前提下减少 token 消耗。

## 5. 配置设计

### 5.1 配置结构

```yaml
listen_addr: ":9101"          # HTTP 监听地址

# Webhook 告警（可选）
webhook:
  url: "https://hooks.example.com/webhook"
  consecutive_failures: 3     # 连续失败 N 次后告警，默认 3

targets:                      # 探测目标列表
  - name: "provider-name"     # 必填，用作 Prometheus label
    endpoint: "https://..."   # 必填，API 端点
    api_key: "${ENV_VAR}"     # API Key，支持环境变量展开
    model: "model-name"       # 必填，模型标识
    api_format: "openai"      # openai | anthropic | google | azure
    prompt: "..."             # 自定义探测 prompt
    prompts: ["A", "B"]       # 多 prompt 轮换（与 prompt 二选一）
    timeout: 30s              # 单次探测超时
    interval: 300s            # 探测间隔
    max_tokens: 20            # 最大输出 token 数
    stream: true              # 是否使用流式（默认 true）
    api_version: "2024-10-21" # Azure API 版本（仅 azure 格式）
    chat_path: "/v1/chat/completions"  # 自定义 API 路径（仅 openai）
    extra_headers:            # 额外 HTTP 头
      X-Custom: "value"
    expect_pattern: "\\d+"    # 响应内容验证（正则表达式）
    # 自适应探测间隔
    adaptive_interval: true   # 启用自适应间隔（默认 false）
    max_interval: 1200s       # 间隔上限（默认 4x interval）
    backoff_after: 5          # 连续成功 N 次后开始 backoff（默认 5）
```

### 5.2 环境变量展开

配置文件在加载时进行 `${VAR_NAME}` 模式的文本替换。这发生在 YAML 解析之前，因此可以用于任何字段值：

```go
var envVarRe = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandEnv(s string) string {
    return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
        key := envVarRe.FindStringSubmatch(match)[1]
        if val, ok := os.LookupEnv(key); ok {
            return val
        }
        return match  // 环境变量不存在时保留原文
    })
}
```

### 5.3 默认值

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `listen_addr` | `:9101` | 避免与 Prometheus Server (9090) 冲突 |
| `api_format` | `openai` | 大多数供应商提供 OpenAI 兼容接口 |
| `prompt` | `Hi` | 最小化 token 消耗 |
| `timeout` | `30s` | 覆盖大多数 LLM 响应时间 |
| `interval` | `300s` | 平衡监控实时性与 API 成本 |
| `max_tokens` | `20` | 足够验证模型可用性，不浪费额度 |
| `stream` | `true` | 流式模式，用于测量 TTFT |
| `api_version` | `2024-10-21` | Azure OpenAI API 版本（仅 azure 格式） |
| `webhook.consecutive_failures` | `3` | 连续失败告警阈值 |
| `adaptive_interval` | `false` | 自适应间隔默认关闭，需要显式启用 |
| `max_interval` | `interval * 4` | 自适应间隔的上限 |
| `backoff_after` | `5` | 连续成功 N 次后开始 backoff |

### 5.4 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--config` | `config.yaml` | 配置文件路径 |
| `--watch-config` | `false` | 监听配置文件变化，自动热重载（推荐 K8s 使用） |
| `--validate` | `false` | 校验配置文件是否合法，然后退出 |
| `--version` | `false` | 打印版本信息并退出 |

## 6. Prometheus 指标

### 6.1 指标定义

| 指标 | 类型 | 说明 |
|------|------|------|
| `llm_probe_success` | Gauge | 1=成功，0=失败 |
| `llm_probe_duration_seconds` | Histogram | 请求总时长 |
| `llm_probe_connect_duration_seconds` | Histogram | DNS+TCP+TLS 连接时间 |
| `llm_probe_ttft_seconds` | Histogram | 首 Token 延迟 |
| `llm_probe_input_tokens` | Gauge | 输入 token 数 |
| `llm_probe_output_tokens` | Gauge | 输出 token 数 |
| `llm_probe_total_tokens` | Gauge | 总 token 数 |
| `llm_probe_token_rate` | Gauge | 生成速率（tok/s） |
| `llm_probe_last_success_timestamp_seconds` | Gauge | 最后一次成功探测的 Unix 时间戳 |
| `llm_probe_errors_total` | Counter | 错误计数（按 error_type） |

### 6.2 Label 设计

基础 labels（所有指标）：

| Label | 来源 | 示例 |
|-------|------|------|
| `provider` | `target.name` | `openai-gpt4o` |
| `model` | `target.model` | `gpt-4o` |
| `endpoint` | `target.endpoint` | `https://api.openai.com` |
| `api_format` | `target.api_format` | `openai` |

`llm_probe_errors_total` 额外标签：

| Label | 说明 | 可选值 |
|-------|------|--------|
| `error_type` | 错误分类 | `timeout`, `canceled`, `auth`, `rate_limit`, `network`, `api_error`, `parse_error`, `validation_error` |

### 6.3 为什么 Token 指标用 Gauge 而非 Counter

Token 消耗指标（`input_tokens`/`output_tokens`/`total_tokens`）使用 Gauge 而非 Counter，因为：
- 这些值表示"最近一次探测的结果"，而非累积量
- 每次探测的 token 量大致相同（固定 prompt + max_tokens），重点是观察是否异常
- Counter 的单调递增语义对"单次探测消耗"无意义

Token rate 同理——它是一个瞬时速率，不是累积计数。

### 6.4 Histogram Bucket 选择

```
Duration:  0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60   (LLM 响应范围 0.1-60s)
Connect:   0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5 (网络连接通常 10ms-5s)
TTFT:      0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20      (首 token 通常 50ms-20s)
```

Bucket 范围根据 LLM API 的实际延迟分布设计，确保 P50/P95/P99 分位数计算的精度。

## 7. 运行时行为

### 7.1 启动流程

```
1. 解析命令行参数（--config, --watch-config, --validate, --version）
2. 加载并验证配置文件
3. 创建 Prometheus Registry（含 Go/Process collectors）
4. 注册所有自定义指标
5. 为每个 target 创建 Prober
6. 启动 Scheduler（每个 target 一个 goroutine，含随机启动 jitter）
7. 如果启用 --watch-config，启动文件监听 goroutine
8. 启动 HTTP Server（/metrics + /healthz + /-/reload + /api/v1/targets + /version）
9. 等待信号：SIGHUP → 热重载，SIGINT/SIGTERM → 优雅关闭
10. 优雅关闭：cancel context → 等待 5s → 退出
```

### 7.2 HTTP 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| `/metrics` | GET | Prometheus 指标采集端点 |
| `/healthz` | GET | 健康检查，始终返回 `200 ok` |
| `/-/reload` | POST | 触发配置热重载，返回 `200 ok` 或 `500` 错误信息 |
| `/api/v1/targets` | GET | 返回所有探测目标的当前状态（JSON 格式） |
| `/version` | GET | 返回版本、commit、构建时间（JSON 格式） |

### 7.3 调度模型

```go
func runTarget(ctx context.Context, target Target, prober Prober) {
    // 随机启动 jitter，分散初始探测负载（0 ~ min(interval, 30s)）
    jitter := randomDuration(min(target.Interval, 30*time.Second))
    time.Sleep(jitter)

    currentInterval := target.Interval
    consecSuccess := 0

    success := probe(ctx, target, prober)       // 首次探测

    // 自适应间隔：稳定时逐步翻倍，失败时立即恢复
    if target.AdaptiveInterval {
        currentInterval, consecSuccess = adaptiveNext(target, success, consecSuccess)
    }

    timer := time.NewTimer(currentInterval)
    for {
        select {
        case <-ctx.Done(): return
        case <-timer.C:
            success = probe(ctx, target, prober)
            if target.AdaptiveInterval {
                currentInterval, consecSuccess = adaptiveNext(target, success, consecSuccess)
            }
            timer.Reset(currentInterval)
        }
    }
}
```

每个 target 在独立的 goroutine 中运行，互不干扰。启动时带有随机 jitter（最多 30 秒），避免大量 target 同时发起首次探测。探测函数 `probe()` 内部使用 `context.WithTimeout` 限制单次探测时间。

**自适应间隔**：启用 `adaptive_interval: true` 后，调度器使用 `time.Timer`（而非固定的 `time.Ticker`），根据探测结果动态调整下一次探测的间隔。连续成功 `backoff_after` 次后每次成功间隔翻倍，封顶于 `max_interval`；任何一次失败立即重置到基础间隔。未启用自适应时行为与固定 ticker 完全一致。

### 7.4 信号处理与优雅关闭

信号处理逻辑：

| 信号 | 行为 |
|------|------|
| `SIGHUP` | 触发配置热重载，不停止服务 |
| `SIGINT` / `SIGTERM` | 触发优雅关闭 |

优雅关闭流程：
1. Cancel root context → 所有探测 goroutine 停止，文件监听退出，进行中的请求被取消
2. 调用 `server.Shutdown()` 完成正在处理的 HTTP 请求
3. 5 秒超时保护，确保进程最终退出

### 7.5 配置热重载

支持三种触发方式，运行时重新加载配置，无需重启进程：

1. **SIGHUP 信号** — `kill -HUP <pid>` 或 `systemctl reload llm-exporter`
2. **HTTP 接口** — `POST /-/reload`
3. **文件监听** — `--watch-config` 启动参数，自动监听配置文件变化

重载流程：

```
1. 重新读取并解析配置文件
2. 调用 Scheduler.Reload():
   a. 先 buildRunners() 验证新配置有效性（含正则编译）
   b. 验证通过后才 Cancel 当前所有探测 goroutine
   c. WaitGroup.Wait() 等待所有 goroutine 退出
   d. 调用 metrics.Reset() 清理旧 target 的指标残留
   e. 替换 targets、probers 和状态缓存
   f. 启动新的探测 goroutine（含 jitter）
3. 日志输出新的 target 列表
```

**文件监听的 Kubernetes 适配**：

Kubernetes 更新 ConfigMap 后，kubelet 通过 symlink 交换目录来更新挂载的文件（而非直接写入）。`watchConfigFile` 监听的是配置文件所在目录（而非文件本身），并监听 `CREATE` 事件来捕获 symlink 交换。内置 2 秒防抖（debounce），避免短时间内多次 reload。

**安全保障**：
- `buildProbers` 先于 `Stop` 执行 — 新配置无效时，旧探测继续运行
- `sync.Mutex` 保护内部状态 — 防止并发 reload
- `sync.WaitGroup` 确保旧 goroutine 完全退出后再启动新的

## 8. 技术选型

### 8.1 为什么用 Go

- Prometheus 生态的原生语言，`client_golang` 是官方库
- 编译为单二进制，部署简单（无 runtime 依赖）
- goroutine 天然适合"每 target 一个探测循环"的并发模型
- 标准库的 `net/http` + `net/http/httptrace` 提供连接级别的计时能力

### 8.2 为什么不用 LLM SDK

直接使用 HTTP + SSE 协议，而非 openai-go / anthropic-sdk：
- **依赖最小化** — 仅 3 个直接依赖（Prometheus + YAML + fsnotify）
- **可控性** — 需要精确控制连接行为（禁用 keep-alive、httptrace 注入）
- **跨协议统一** — 三种 API 格式共享 SSE 解析器，SDK 反而增加抽象层
- **编译体积** — 最终二进制约 10MB，无多余依赖

### 8.3 依赖清单

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/prometheus/client_golang` | v1.20.5 | Prometheus 指标注册与 HTTP handler |
| `gopkg.in/yaml.v3` | v3.0.1 | 配置文件解析 |
| `github.com/fsnotify/fsnotify` | v1.9.0 | 配置文件变更监听（`--watch-config`） |

仅 3 个直接依赖，无 CGO，交叉编译友好。

## 9. 部署架构

### 9.1 单机部署

```
┌──────────────────────┐     scrape      ┌──────────────┐
│   LLM Exporter       │ ◄───────────── │  Prometheus   │
│   :9101               │                │  :9090        │
└──────────┬───────────┘                └──────┬───────┘
           │ probe                              │ query
           ▼                                    ▼
     ┌──────────┐                        ┌──────────┐
     │ LLM APIs │                        │ Grafana  │
     └──────────┘                        │ :3000    │
                                         └──────────┘
```

### 9.2 多区域部署

在不同网络出口部署多个 Exporter 实例，对比不同区域到同一 LLM API 的延迟差异：

```
                        ┌── LLM Exporter (us-east)  ──┐
                        │   probe → OpenAI/Anthropic   │
                        └──────────────────────────────┘
┌──────────────┐                                          ┌──────────┐
│  Prometheus  │ ◄── scrape ──────────────────────────── │ Grafana  │
└──────────────┘                                          └──────────┘
                        ┌── LLM Exporter (ap-southeast)──┐
                        │   probe → OpenAI/Anthropic      │
                        └──────────────────────────────────┘
```

Prometheus 通过 `region` label 区分数据来源。

### 9.3 Kubernetes 部署

- Deployment（replicas=1，无需多副本）
- ConfigMap 存配置文件，Secret 存 API Key
- 启动参数 `--watch-config`：ConfigMap 更新后自动重载，无需重启 Pod
- ServiceMonitor（Prometheus Operator 自动发现）
- liveness/readiness probe → `/healthz`
- 资源建议：50m CPU / 64Mi Memory（requests），200m / 128Mi（limits）

## 10. Grafana 面板设计

### 10.1 面板结构

预置的 Grafana Dashboard（`grafana/dashboards/llm-exporter.json`）分为四个区域：

| 区域 | 面板数 | 说明 |
|------|--------|------|
| Overview | 9 stat | 全局概览：状态地图、目标总数、在线数、可用率、TTFT/连接/速率/时长均值、错误数 |
| Target Detail | 11 stat + 8 时序图 | 按 `provider` 重复，展示单目标的完整指标和趋势 |
| Cross-Provider | 6 时序图 | 跨供应商横向对比：TTFT P95、Connect P95、Duration P95、Token Rate、Availability、Error Rate |
| TTFT Heatmap | 1 热力图 | 默认折叠，TTFT 分布热力图，用于识别延迟模式和异常集中 |

### 10.2 模板变量

| 变量 | 查询 | 说明 |
|------|------|------|
| `datasource` | — | Prometheus 数据源选择器 |
| `provider` | `label_values(llm_probe_success, provider)` | 按目标名称筛选，多选 |
| `model` | `label_values(llm_probe_success{provider=~"$provider"}, model)` | 级联自 provider，按模型筛选 |

`model` 变量级联自 `provider`，选择特定供应商后只显示该供应商的模型列表。

### 10.3 查询规范

面板查询遵循以下规范，确保在各种数据条件下正确渲染：

**Rate Interval**：所有 `rate()` / `increase()` 函数使用 `$__rate_interval` 而非硬编码的 `[5m]`，确保在不同 scrape_interval 下正确计算。

**除法安全**：所有包含除法的查询使用 `clamp_min(divisor, 1e-10)` 保护，防止分母为零导致面板显示 NaN：

```promql
# 示例：TTFT 均值
rate(llm_probe_ttft_seconds_sum[$__rate_interval])
  / clamp_min(rate(llm_probe_ttft_seconds_count[$__rate_interval]), 1e-10)
```

**Request Phases 减法保护**：三阶段分解中的减法使用 `clamp_min(..., 0)` 避免负值：

```promql
# Wait = TTFT - Connect，可能因采样时间差出现负值
clamp_min(
  rate(llm_probe_ttft_seconds_sum[...]) / clamp_min(rate(llm_probe_ttft_seconds_count[...]), 1e-10)
  - rate(llm_probe_connect_duration_seconds_sum[...]) / clamp_min(rate(llm_probe_connect_duration_seconds_count[...]), 1e-10)
, 0)
```

**变量过滤**：所有面板（包括 Overview 和 Cross-Provider）统一使用 `{provider=~"$provider", model=~"$model"}` 筛选，确保模板变量全局生效。

### 10.4 面板说明

每个面板标题旁均附有信息图标（ℹ），鼠标悬停显示中文说明，帮助用户理解指标含义。说明内容存储在面板的 `description` 字段中，示例：

| 面板 | 说明 |
|------|------|
| Status Map | 所有目标的当前探测状态 |
| Avg TTFT | 所有目标的平均首 Token 延迟（Time to First Token） |
| Connect | 连接建立耗时均值（DNS + TCP + TLS） |
| Token Rate | Token 生成速率（output_tokens / 生成时长，tok/s） |
| Request Phases | 请求阶段分解：Connect → Wait → Generation |
| TTFT Heatmap | 所有供应商 TTFT 直方图分布热力图，用于识别延迟集中区间 |

### 10.5 设计考量

- **Target Detail 按 provider 重复** — 使用 Grafana 的 row repeat 功能，每个供应商自动生成一组完整面板
- **默认折叠 Heatmap** — TTFT 热力图占用较大空间且属于进阶分析，默认折叠减少页面滚动
- **Cross-Provider 横向对比** — 6 个时序图等宽排列，同一时间轴便于直观比较不同供应商的表现差异
- **stat 面板配色** — 使用阈值色彩映射（绿/黄/红），一眼识别异常状态

## 11. 与 Blackbox Exporter 的对比

| 维度 | Blackbox Exporter | LLM Exporter |
|------|-------------------|--------------|
| 定位 | 通用 HTTP/TCP/DNS/ICMP 拨测 | LLM API 专用探测 |
| 触发方式 | 被动（Prometheus scrape 触发） | 主动（内部 ticker 调度） |
| 协议理解 | HTTP 状态码 + TLS 证书 | SSE 流式协议 + Token 解析 |
| 延迟测量 | DNS/Connect/TLS/FirstByte/Total | Connect/TTFT/Duration（三阶段） |
| 可测指标 | 连通性、证书过期、重定向 | 可用性、TTFT、Token 消耗、生成速率 |
| 超时处理 | 受 scrape_timeout 约束 | 每 target 独立 timeout |
| 适用时长 | 毫秒级检查 | 秒-分钟级探测 |
| 配置重载 | `/-/reload` + SIGHUP | `/-/reload` + SIGHUP + 文件监听（K8s 友好） |

两者互补：Blackbox Exporter 检查 API 端点的网络可达性和 TLS 健康，LLM Exporter 验证模型服务的实际可用性。

## 12. 已实现的扩展功能

以下功能在 v2.0.0 中实现：

- **探测结果缓存 API** — `/api/v1/targets` 接口返回各 target 最近探测结果的 JSON 格式，包含连续失败次数、总探测/成功次数等
- **Webhook 告警** — 连续 N 次失败时自动发送 webhook POST 请求，支持 Slack/飞书/钉钉等任何接受 JSON 的 webhook
- **多 prompt 轮换** — `prompts` 配置多个 prompt 轮换使用，避免 provider 缓存优化影响测量
- **输出校验** — `expect_pattern` 正则校验响应内容，检测模型是否返回预期内容
- **Azure OpenAI 支持** — `api_format: azure`，使用 `api-key` 认证和 Azure 特有的 URL 结构
- **非流式探测** — `stream: false` 配置，支持不需要流式的场景（OpenAI/Azure 格式）
- **版本信息注入** — 通过 ldflags 注入 git commit/version/build time，`--version` 和 `/version` 端点
- **配置校验** — `--validate` 参数，校验配置文件后退出，便于 CI/CD 集成
- **启动 jitter** — 随机延迟 0~30s 分散初始探测，避免同时大量请求
- **自适应探测间隔** — `adaptive_interval: true` 启用，服务稳定时自动拉长间隔（翻倍至 `max_interval`），故障时立即恢复基础间隔，可减少 50%+ 的 token 消耗

### 未来扩展方向

- **Grafana 面板增强** — 添加 `llm_probe_last_success_timestamp_seconds` 相关面板
- **多区域对比** — 通过 Prometheus `external_labels` 区分部署区域，面板级横向对比
- **Light probe 与 prompt rotation 联动** — light 模式下也支持 prompt 轮换
- **自定义 webhook payload 模板** — 支持用户自定义告警消息格式
