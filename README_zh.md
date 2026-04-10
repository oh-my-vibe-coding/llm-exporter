# LLM Exporter

[English](README.md) | **中文**

> **100% AI 编码** — 本项目的所有代码完全由 AI（Claude Code）编写。人类仅参与产品方向、需求定义和代码审查。

Prometheus Exporter，用于监控大语言模型 API 的可用性和性能。定期向 LLM API 端点发起流式探测请求，采集网络延迟、首 Token 延迟（TTFT）和 Token 消耗量。

类似于 [blackbox-exporter](https://github.com/prometheus/blackbox_exporter) 对 HTTP 端点的拨测，LLM Exporter 专注于大模型 API 的端到端可用性探测。

## 功能特性

- **流式 TTFT 测量** — 所有 API 均使用 streaming 请求，精确测量首 Token 到达时间
- **多 API 格式支持** — OpenAI / Anthropic / Google Gemini / Azure OpenAI，以及所有 OpenAI 兼容的服务
- **国内外厂商** — 阿里百炼、字节方舟、DeepSeek、Mistral、OpenRouter 等开箱可用
- **轻量少依赖** — 仅依赖 `prometheus/client_golang`、`gopkg.in/yaml.v3` 和 `fsnotify/fsnotify`，不引入任何 LLM SDK
- **单二进制部署** — Go 编译，支持二进制 / Docker / Kubernetes 部署
- **灵活配置** — YAML 配置，支持 `${ENV_VAR}` 环境变量展开、自定义请求头和 API 路径
- **配置热重载** — 支持 SIGHUP 信号、HTTP `/-/reload` 接口、`--watch-config` 文件监听三种方式
- **Webhook 告警** — 连续探测失败后自动发送 webhook 通知（Slack/飞书/钉钉等）
- **Prompt 轮换** — 配置多个 prompt 轮流使用，避免 provider 缓存
- **响应验证** — 正则匹配模型输出内容，确认模型真正在正常工作
- **自适应探测间隔** — 服务稳定时自动降低探测频率节省 token，故障时立即恢复高频探测
- **非流式探测** — 支持 `stream: false` 模式，兼容不支持流式的 API
- **运维友好** — `--validate` 配置校验、`--version` 版本信息、`/api/v1/targets` 状态 API

## 快速开始

### 二进制运行

```bash
# 构建
make build

# 编辑配置
cp config.example.yaml config.yaml
# 修改 config.yaml，填入你的 API Key

# 设置环境变量（或直接写入 config.yaml）
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."

# 运行
./llm-exporter --config config.yaml

# 验证
curl http://localhost:9101/metrics | grep llm_probe
curl http://localhost:9101/healthz
```

### Docker Compose（含 Prometheus + Grafana）

```bash
# 配置 API Key
cp .env.example .env
# 编辑 .env 填入 Key

# 启动全套监控栈
docker compose up --build -d

# 访问
# LLM Exporter:  http://localhost:9101/metrics
# Prometheus:    http://localhost:9090
# Grafana:       http://localhost:3000  (admin/admin)
```

Grafana 启动后会自动加载预置的 LLM 监控面板。

## 部署方式

### 方式一：Systemd

适用于在 Linux 服务器上直接部署的场景。

**1. 安装二进制**

```bash
# 构建
make build

# 安装到系统路径
sudo cp llm-exporter /usr/local/bin/
sudo chmod +x /usr/local/bin/llm-exporter

# 创建配置目录
sudo mkdir -p /etc/llm-exporter
sudo cp config.example.yaml /etc/llm-exporter/config.yaml
# 编辑配置文件，填入实际的 API Key 和目标
sudo vim /etc/llm-exporter/config.yaml
```

**2. 创建系统用户**

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin llm-exporter
```

**3. 创建 systemd service 文件**

```bash
sudo cat > /etc/systemd/system/llm-exporter.service << 'EOF'
[Unit]
Description=LLM Exporter - Prometheus LLM API Probe Exporter
Documentation=https://github.com/oh-my-vibe-coding/llm-exporter
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=llm-exporter
Group=llm-exporter
ExecStart=/usr/local/bin/llm-exporter --config /etc/llm-exporter/config.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# 安全加固
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadOnlyPaths=/etc/llm-exporter
PrivateTmp=yes

# 环境变量（可选，也可以写在配置文件里）
EnvironmentFile=-/etc/llm-exporter/env

[Install]
WantedBy=multi-user.target
EOF
```

**4. 环境变量文件（可选）**

如果配置中使用了 `${ENV_VAR}` 引用：

```bash
sudo cat > /etc/llm-exporter/env << 'EOF'
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
EOF
sudo chmod 600 /etc/llm-exporter/env
sudo chown llm-exporter:llm-exporter /etc/llm-exporter/env
```

**5. 启动服务**

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now llm-exporter

# 查看状态
sudo systemctl status llm-exporter

# 查看日志
sudo journalctl -u llm-exporter -f
```

### 方式二：Docker

**单容器运行：**

```bash
docker run -d \
  --name llm-exporter \
  --restart unless-stopped \
  -p 9101:9101 \
  -v $(pwd)/config.yaml:/etc/llm-exporter/config.yaml:ro \
  --env-file .env \
  llm-exporter
```

**Docker Compose（含完整监控栈）：**

```bash
docker compose up --build -d
```

包含三个服务：
| 服务 | 端口 | 说明 |
|------|------|------|
| llm-exporter | 9101 | Exporter 指标端点 |
| prometheus | 9090 | Prometheus Server |
| grafana | 3000 | Grafana（admin/admin），自动加载面板 |

### 方式三：Kubernetes

**1. 创建 ConfigMap 和 Secret**

```yaml
# llm-exporter-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: llm-exporter-config
  namespace: monitoring
data:
  config.yaml: |
    listen_addr: ":9101"
    targets:
      - name: openai-gpt4o
        endpoint: "https://api.openai.com"
        api_key: "${OPENAI_API_KEY}"
        model: "gpt-4o"
        api_format: openai
        timeout: 30s
        interval: 60s
---
apiVersion: v1
kind: Secret
metadata:
  name: llm-exporter-secrets
  namespace: monitoring
type: Opaque
stringData:
  OPENAI_API_KEY: "sk-..."
  ANTHROPIC_API_KEY: "sk-ant-..."
```

**2. Deployment + Service**

```yaml
# llm-exporter-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: llm-exporter
  namespace: monitoring
  labels:
    app: llm-exporter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: llm-exporter
  template:
    metadata:
      labels:
        app: llm-exporter
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9101"
        prometheus.io/path: "/metrics"
    spec:
      containers:
        - name: llm-exporter
          image: llm-exporter:latest
          args: ["--config", "/etc/llm-exporter/config.yaml", "--watch-config"]
          ports:
            - containerPort: 9101
              name: metrics
          envFrom:
            - secretRef:
                name: llm-exporter-secrets
          volumeMounts:
            - name: config
              mountPath: /etc/llm-exporter
              readOnly: true
          livenessProbe:
            httpGet:
              path: /healthz
              port: 9101
            initialDelaySeconds: 5
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /healthz
              port: 9101
            initialDelaySeconds: 3
            periodSeconds: 10
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
      volumes:
        - name: config
          configMap:
            name: llm-exporter-config
---
apiVersion: v1
kind: Service
metadata:
  name: llm-exporter
  namespace: monitoring
  labels:
    app: llm-exporter
spec:
  selector:
    app: llm-exporter
  ports:
    - port: 9101
      targetPort: metrics
      name: metrics
```

**3. ServiceMonitor（Prometheus Operator）**

如果使用 Prometheus Operator，创建 ServiceMonitor 自动发现：

```yaml
# llm-exporter-servicemonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: llm-exporter
  namespace: monitoring
  labels:
    release: prometheus   # 需匹配你的 Prometheus Operator selector
spec:
  selector:
    matchLabels:
      app: llm-exporter
  endpoints:
    - port: metrics
      interval: 15s
      path: /metrics
```

**4. 部署**

```bash
kubectl apply -f llm-exporter-config.yaml
kubectl apply -f llm-exporter-deployment.yaml
kubectl apply -f llm-exporter-servicemonitor.yaml   # 如果使用 Prometheus Operator
```

## 配置热重载

支持三种方式在不重启进程的情况下重新加载配置文件：

**方式一：SIGHUP 信号**

```bash
# systemd 部署
sudo systemctl reload llm-exporter

# 手动发信号
kill -HUP $(pidof llm-exporter)

# Docker
docker kill --signal=HUP llm-exporter
```

**方式二：HTTP 接口**

```bash
curl -X POST http://localhost:9101/-/reload
```

**方式三：文件监听（推荐用于 Kubernetes）**

```bash
llm-exporter --config /etc/llm-exporter/config.yaml --watch-config
```

启用 `--watch-config` 后，程序会监听配置文件所在目录的文件变化，检测到更新后自动重载。专门处理了 Kubernetes ConfigMap 的 symlink 交换机制，并内置 2 秒防抖避免短时间内多次重载。

重载时会停止当前所有探测 goroutine，重新解析配置文件，然后启动新的探测。如果新配置有错误，重载失败并记录日志，现有探测不受影响。

## 配置说明

### 配置文件格式

```yaml
listen_addr: ":9101"      # 监听地址，默认 :9101

# Webhook 告警（可选）：连续探测失败后发送 POST 请求
# webhook:
#   url: "https://hooks.slack.com/services/xxx"
#   consecutive_failures: 3  # 默认 3 次

targets:
  - name: openai-gpt4o    # 目标名称（必填，用作 provider label）
    endpoint: "https://api.openai.com"   # API 端点（必填）
    api_key: "${OPENAI_API_KEY}"         # API Key，支持环境变量
    model: "gpt-4o"                      # 模型名称（必填）
    prompt: "Hi"                         # 探测 prompt，默认 "Hi"
    api_format: openai                   # API 格式：openai / anthropic / google / azure
    timeout: 30s                         # 单次探测超时，默认 30s
    interval: 300s                       # 探测间隔，默认 300s
    max_tokens: 20                       # 最大生成 token 数，默认 20
    chat_path: "/v1/chat/completions"    # 自定义 API 路径（仅 openai 格式）
    extra_headers:                       # 自定义请求头
      X-Custom-Auth: "token"
    # stream: false                      # 非流式模式（默认 true）
    # api_version: "2024-10-21"          # Azure API 版本（仅 azure 格式）
    # prompts: ["Hi", "Hello", "Hey"]    # Prompt 轮换（与 prompt 二选一）
    # expect_pattern: "\\d+"             # 响应内容验证（正则表达式）
    # adaptive_interval: true            # 自适应间隔（默认关闭）
    # max_interval: 1200s                # 自适应上限，默认 4x interval
    # backoff_after: 5                   # 连续成功 N 次后开始 backoff，默认 5
```

### 各平台配置示例

#### OpenAI

```yaml
- name: openai-gpt4o
  endpoint: "https://api.openai.com"
  api_key: "${OPENAI_API_KEY}"
  model: "gpt-4o"
  api_format: openai
```

#### Anthropic Claude

```yaml
- name: anthropic-claude
  endpoint: "https://api.anthropic.com"
  api_key: "${ANTHROPIC_API_KEY}"
  model: "claude-sonnet-4-20250514"
  api_format: anthropic
```

#### Google Gemini

```yaml
- name: google-gemini
  endpoint: "https://generativelanguage.googleapis.com"
  api_key: "${GOOGLE_API_KEY}"
  model: "gemini-2.0-flash"
  api_format: google
```

#### 阿里百炼（DashScope）

百炼提供 OpenAI 兼容接口，直接使用 `openai` 格式：

```yaml
- name: dashscope-qwen
  endpoint: "https://dashscope.aliyuncs.com/compatible-mode"
  api_key: "${DASHSCOPE_API_KEY}"
  model: "qwen-plus"
  api_format: openai
```

#### 字节方舟（Ark）

方舟同样兼容 OpenAI 协议，`model` 字段填写接入点 ID（Endpoint ID）：

```yaml
- name: ark-pro
  endpoint: "https://ark.cn-beijing.volces.com/api"
  api_key: "${ARK_API_KEY}"
  model: "${ARK_ENDPOINT_ID}"
  api_format: openai
```

#### 自定义代理 API

对于使用自定义中转/代理的场景，可通过 `chat_path` 和 `extra_headers` 灵活适配：

```yaml
- name: my-proxy
  endpoint: "https://my-proxy.example.com"
  api_key: "${PROXY_API_KEY}"
  model: "gpt-4o"
  api_format: openai
  chat_path: "/api/v1/chat"          # 代理可能使用不同的路径
  extra_headers:
    X-Proxy-Token: "${PROXY_TOKEN}"  # 额外认证头
```

#### 本地 Ollama

```yaml
- name: local-ollama
  endpoint: "http://localhost:11434"
  model: "llama3"
  api_format: openai
  interval: 30s
```

#### Azure OpenAI

```yaml
- name: azure-gpt4o
  endpoint: "${AZURE_OPENAI_ENDPOINT}"
  api_key: "${AZURE_OPENAI_API_KEY}"
  model: "gpt-4o"
  api_format: azure
  # api_version: "2024-10-21"  # 默认值
```

#### OpenRouter

```yaml
- name: openrouter-claude
  endpoint: "https://openrouter.ai/api"
  api_key: "${OPENROUTER_API_KEY}"
  model: "anthropic/claude-sonnet-4"
  api_format: openai
```

#### DeepSeek

```yaml
- name: deepseek-chat
  endpoint: "https://api.deepseek.com"
  api_key: "${DEEPSEEK_API_KEY}"
  model: "deepseek-chat"
  api_format: openai
```

#### Mistral

```yaml
- name: mistral-large
  endpoint: "https://api.mistral.ai"
  api_key: "${MISTRAL_API_KEY}"
  model: "mistral-large-latest"
  api_format: openai
```

### 环境变量展开

配置文件中所有 `${VAR_NAME}` 格式的值会在加载时自动替换为对应的环境变量值。如果环境变量不存在，则保留原始字符串。

## Prometheus 指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `llm_probe_success` | Gauge | 最近一次探测是否成功（1=成功, 0=失败） |
| `llm_probe_duration_seconds` | Histogram | 完整请求时长（从发起请求到流结束） |
| `llm_probe_connect_duration_seconds` | Histogram | 连接建立耗时（DNS + TCP + TLS） |
| `llm_probe_ttft_seconds` | Histogram | 首 Token 延迟（从发起请求到收到第一个内容 token） |
| `llm_probe_input_tokens` | Gauge | 本次探测的输入 token 数 |
| `llm_probe_output_tokens` | Gauge | 本次探测的输出 token 数 |
| `llm_probe_total_tokens` | Gauge | 本次探测消耗的 token 总数 |
| `llm_probe_token_rate` | Gauge | Token 生成速率（output_tokens / generation_time, tok/s） |
| `llm_probe_last_success_timestamp_seconds` | Gauge | 最后一次探测成功的 Unix 时间戳 |
| `llm_probe_errors_total` | Counter | 累计错误次数，按 `error_type` 分类 |

### Labels

所有指标携带以下标签：

| Label | 说明 |
|-------|------|
| `provider` | 目标名称（对应配置中的 `name`） |
| `model` | 模型名称 |
| `endpoint` | API 端点地址 |
| `api_format` | API 格式（openai / anthropic / google / azure） |

`llm_probe_errors_total` 额外携带 `error_type` 标签：

| error_type | 说明 |
|------------|------|
| `timeout` | 请求超时 |
| `canceled` | 请求被取消（热重载或关闭期间） |
| `auth` | 认证失败（HTTP 401/403） |
| `rate_limit` | 限流（HTTP 429） |
| `network` | 网络错误（DNS 解析失败、连接拒绝等） |
| `api_error` | API 返回其他错误状态码 |
| `parse_error` | 响应解析失败或流中无内容 |
| `validation_error` | 响应内容未匹配 `expect_pattern` 正则 |

### Histogram Buckets

```
llm_probe_duration_seconds:         0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60
llm_probe_connect_duration_seconds: 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5
llm_probe_ttft_seconds:             0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20
```

## Prometheus 配置

### 基础配置

```yaml
# prometheus.yml
scrape_configs:
  - job_name: llm-exporter
    scrape_interval: 15s
    static_configs:
      - targets: ["localhost:9101"]
```

### 带 relabel 的配置（推荐）

当多实例部署时，为方便区分来源：

```yaml
scrape_configs:
  - job_name: llm-exporter
    scrape_interval: 15s
    static_configs:
      - targets: ["llm-exporter-1:9101"]
        labels:
          region: "us-east"
      - targets: ["llm-exporter-2:9101"]
        labels:
          region: "ap-southeast"
```

### 告警规则示例

```yaml
# alerts.yml
groups:
  - name: llm-probe
    rules:
      # 某个目标连续 5 分钟探测失败
      - alert: LLMProbeDown
        expr: llm_probe_success == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "LLM endpoint {{ $labels.provider }} is down"
          description: "Provider {{ $labels.provider }} (model={{ $labels.model }}) has been failing probes for 5 minutes."

      # TTFT P99 超过 10 秒
      - alert: LLMHighTTFT
        expr: histogram_quantile(0.99, sum(rate(llm_probe_ttft_seconds_bucket[10m])) by (le, provider, model)) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High TTFT for {{ $labels.provider }}"
          description: "P99 TTFT for {{ $labels.provider }} (model={{ $labels.model }}) is {{ $value | humanizeDuration }}."

      # 错误率 > 30%
      - alert: LLMHighErrorRate
        expr: rate(llm_probe_errors_total[10m]) / rate(llm_probe_duration_seconds_count[10m]) > 0.3
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High error rate for {{ $labels.provider }}"

      # 限流告警
      - alert: LLMRateLimited
        expr: increase(llm_probe_errors_total{error_type="rate_limit"}[10m]) > 3
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.provider }} is being rate limited"
```

## PromQL 查询示例

### 可用性

```promql
# 当前状态（即时值，1=UP / 0=DOWN）
llm_probe_success

# 5 分钟可用率
avg_over_time(llm_probe_success[5m])

# 24 小时可用率
avg_over_time(llm_probe_success[24h])

# 全局可用率（所有目标汇总）
count(llm_probe_success == 1) / count(llm_probe_success)
```

### 延迟

```promql
# TTFT P50 / P95 / P99
histogram_quantile(0.50, sum(rate(llm_probe_ttft_seconds_bucket[5m])) by (le, provider, model))
histogram_quantile(0.95, sum(rate(llm_probe_ttft_seconds_bucket[5m])) by (le, provider, model))
histogram_quantile(0.99, sum(rate(llm_probe_ttft_seconds_bucket[5m])) by (le, provider, model))

# 总请求时长 P95
histogram_quantile(0.95, sum(rate(llm_probe_duration_seconds_bucket[5m])) by (le, provider, model))

# TTFT 平均值
rate(llm_probe_ttft_seconds_sum[5m]) / rate(llm_probe_ttft_seconds_count[5m])
```

### 错误

```promql
# 错误速率（按类型）
sum(rate(llm_probe_errors_total[5m])) by (provider, error_type)

# 各 provider 总错误率
sum(rate(llm_probe_errors_total[5m])) by (provider)

# 限流事件
rate(llm_probe_errors_total{error_type="rate_limit"}[5m])
```

### Token 消耗

```promql
# 各目标最近探测的 token 消耗
llm_probe_total_tokens
llm_probe_input_tokens
llm_probe_output_tokens
```

### 连接与生成性能

```promql
# 连接耗时 P95（DNS + TCP + TLS）
histogram_quantile(0.95, sum(rate(llm_probe_connect_duration_seconds_bucket[5m])) by (le, provider, model))

# Token 生成速率（tok/s）
llm_probe_token_rate
```

## Grafana 面板

项目包含一个预置的 Grafana Dashboard JSON（`grafana/dashboards/llm-exporter.json`），设计参考了 blackbox-exporter 的 HTTP 拨测面板风格。

### 面板布局

```
┌──────────────────────────────────────────────────────────────────────────┐
│  Overview                                                                │
├──────────────────────────────────────────────────────────────────────────┤
│ Status │ Total │ Online │Avail% │ TTFT │Connect│TokRate│Duration│Errors │
│  Map   │       │        │       │      │       │       │        │ (5m)  │
├──────────────────────────────────────────────────────────────────────────┤
│  Target Detail (按 provider 重复)                                        │
├──────┬──────┬──────┬──────┬──────┬──────┬──────┬──────┬──────┬───┬──────┤
│ 状态  │ 24h  │ TTFT │连接  │时长  │速率  │ In   │ Out  │Total │Err│Model │
│      │可用率 │      │耗时  │      │tok/s │ Tok  │ Tok  │ Tok  │   │      │
├──────┴──────┴──────┴──────┴──────┴──────┴──────┴──────┴──────┴───┴──────┤
│  Request Phases (Connect/Wait/Gen) │  Connect Latency P50/P95/P99       │
│  TTFT P50/P95/P99                  │  Token Rate                        │
│  Duration P50/P95/P99              │  Input/Output Tokens               │
│  Probe Success Timeline            │  Errors by Type                    │
├──────────────────────────────────────────────────────────────────────────┤
│  Cross-Provider Comparison                                               │
├──────────────────────────────────────────────────────────────────────────┤
│ TTFT P95 │ Connect P95 │ Duration P95 │ Token Rate │ Avail 1h │ ErrRate │
├──────────────────────────────────────────────────────────────────────────┤
│  TTFT Heatmap (默认折叠)                                                 │
├──────────────────────────────────────────────────────────────────────────┤
│ TTFT Distribution Heatmap                                                │
└──────────────────────────────────────────────────────────────────────────┘
```

### 面板功能

| 区域 | 说明 |
|------|------|
| **Overview** | 全局概览，9 个 stat 面板展示所有目标的汇总状态 |
| **Target Detail** | 按 `provider` 重复，每个目标展示 11 个 stat 指标 + 8 个时序图 |
| **Cross-Provider** | 跨供应商对比，6 个时序图并排便于横向比较 |
| **TTFT Heatmap** | 默认折叠，展开后显示 TTFT 分布热力图，识别延迟模式 |

### 模板变量

面板支持两个筛选变量：
- **provider** — 按目标名称筛选（级联自 `llm_probe_success` 的 `provider` label）
- **model** — 按模型筛选（级联自 `provider` 选择）

### 面板说明

每个面板标题旁有一个小问号图标（ℹ），鼠标悬停可查看该指标的中文说明，包括指标含义、计算方式和单位。

### 手动导入

1. 打开 Grafana → Dashboards → Import
2. 上传 `grafana/dashboards/llm-exporter.json`
3. 选择 Prometheus 数据源
4. 点击 Import

### Docker Compose 自动加载

使用本项目的 `docker-compose.yml` 时，Grafana 会通过 provisioning 自动加载面板，无需手动导入。

## 项目结构

```
llm-exporter/
├── cmd/llm-exporter/main.go        # 入口：flag 解析、HTTP server、信号处理、文件监听
├── internal/
│   ├── config/config.go             # YAML 配置解析 + 环境变量展开
│   ├── metrics/metrics.go           # Prometheus 指标定义
│   ├── prober/
│   │   ├── prober.go                # Prober 接口 + 工厂函数 + 连接追踪
│   │   ├── sse.go                   # SSE 事件流解析器
│   │   ├── errors.go                # 错误分类（timeout/auth/rate_limit/...）
│   │   ├── openai.go                # OpenAI 兼容探测（流式/非流式）
│   │   ├── anthropic.go             # Anthropic 流式探测
│   │   ├── google.go                # Google Gemini 流式探测
│   │   └── azure.go                 # Azure OpenAI 探测（流式/非流式）
│   ├── scheduler/scheduler.go       # 调度 + 热重载 + 告警 + 响应验证
│   └── version/version.go           # 版本信息（通过 ldflags 注入）
├── grafana/
│   ├── provisioning/
│   │   ├── dashboards/dashboards.yml
│   │   └── datasources/datasources.yml
│   └── dashboards/
│       └── llm-exporter.json        # 预置 Grafana 面板
├── config.example.yaml
├── prometheus.yml
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── DESIGN.md                        # 设计文档
└── go.mod
```

## TTFT 测量原理

所有 API 均使用**流式（streaming）**模式请求，TTFT 的计时起点为 HTTP 请求发出的瞬间：

| API 格式 | 请求方式 | TTFT 判定条件 |
|----------|---------|---------------|
| OpenAI | `POST /v1/chat/completions` + `stream: true` | 首个 SSE 事件中 `choices[0].delta.content` 非空 |
| Anthropic | `POST /v1/messages` + `stream: true` | 首个 `content_block_delta` 事件的 `text_delta` 非空 |
| Google | `POST /v1beta/models/{model}:streamGenerateContent?alt=sse` | 首个含文本内容的 `candidate` |
| Azure | `POST /openai/deployments/{model}/chat/completions` + `stream: true` | 同 OpenAI 格式 |

## 探测方法论

为了获得科学、可靠的测量数据，LLM Exporter 在探测设计上做了以下优化：

| 策略 | 说明 |
|------|------|
| **禁用连接复用** | 每次探测创建新的 TCP 连接（`DisableKeepAlives`），真实反映 DNS + TCP + TLS 耗时 |
| **Prompt 随机化** | 自动在 prompt 末尾追加时间戳 `[t=<unix_millis>]`，防止代理/CDN 缓存导致测量失真 |
| **可配置输出量** | 默认 `max_tokens=20`，可按需调大以获得更准确的 token rate 数据 |
| **Token Rate 阈值保护** | 仅在输出 ≥5 tokens 且生成时间 ≥100ms 时才计算 token rate，避免除以极小数产生异常值 |
| **三阶段时间分解** | Connect（DNS+TCP+TLS）→ Wait（TTFT - Connect）→ Generation（Duration - TTFT） |

## 自适应探测间隔

启用 `adaptive_interval: true` 后，探测间隔会根据目标的健康状态动态调整：

- **稳定期**：连续成功 `backoff_after` 次（默认 5）后，每次成功间隔翻倍（如 5m → 10m → 20m），封顶于 `max_interval`（默认 4 倍基础间隔）
- **故障时**：任何一次探测失败立即重置到基础间隔，确保故障检测不受影响

```yaml
# 示例配置
- name: openai-gpt4o
  endpoint: "https://api.openai.com"
  api_key: "${OPENAI_API_KEY}"
  model: "gpt-4o"
  interval: 300s
  adaptive_interval: true     # 默认 false
  max_interval: 1200s         # 默认 4x interval
  backoff_after: 5            # 默认 5
```

**对准确性的影响**：每个探测数据点本身完全准确（延迟、TTFT、token rate 不受影响），唯一代价是稳定期采样密度降低。间隔变化时会输出日志：

```
[openai-gpt4o] adaptive interval: 5m0s -> 10m0s (consec_success=6)
```

与 light probe 模式组合使用可实现最大节省。例如 `interval=300s` + `adaptive_interval=true` + `full_probe_every=10`，稳定期 token 消耗可降低 80% 以上。

## 构建

```bash
# 本地构建（自动注入版本信息）
make build

# 查看版本
./llm-exporter --version

# 校验配置
./llm-exporter --validate --config config.yaml

# Docker 镜像
make docker

# 运行测试
make test

# 清理
make clean
```

## HTTP 端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/metrics` | GET | Prometheus 指标 |
| `/healthz` | GET | 健康检查，返回 "ok" |
| `/-/reload` | POST | 触发配置热重载 |
| `/api/v1/targets` | GET | 返回所有探测目标的当前状态（JSON） |
| `/version` | GET | 返回版本信息（JSON） |

## CLI 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--config` | `config.yaml` | 配置文件路径 |
| `--watch-config` | `false` | 监听配置文件变化并自动重载 |
| `--validate` | `false` | 校验配置文件是否合法，然后退出 |
| `--version` | `false` | 打印版本信息并退出 |

## License

MIT
