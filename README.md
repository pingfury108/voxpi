# VoxPi

树莓派上的离线 TTS 服务，OpenAI 兼容接口，单机低并发。

- 推理：sherpa-onnx（v1.13.8，pin）加载 Piper / Matcha ONNX，纯 CPU
- 服务：Go + `net/http` + 薄 cgo，单二进制 + 单 worker 队列
- 接口：`POST /v1/audio/speech`、`GET /v1/models`、`GET /healthz`

## 评估结论（Pi 400 / 4GB 实测）

方案整体成立，实测比预期更好：

| 项目 | 预估 | 实测 |
|---|---|---|
| Piper 中文 `zh_CN-huayan-medium` RTF | 0.35–0.6 | **0.36–0.39** |
| Piper 英文 `en_US-lessac-medium` RTF | 0.357 | **0.42** |
| Matcha `zh-baker` RTF | 0.5–1.0 | **0.38** |
| 模型加载 | — | 中文 6–8s，英文 3s |
| 内存（RSS） | 模型 200MB + Go 50MB | 空闲 105MB，跑完长文本 250–390MB |
| 温度 | 长文本会降频 | 44–49℃，`get_throttled=0x0`，未降频 |

具体测量（`num_threads=4`，单请求串行）：

| 用例 | 字符 | 音频时长 | 生成耗时 | RTF |
|---|---|---|---|---|
| 中文 Piper | 51 | 8.1s | 3.3s | 0.41 |
| 中文 Piper | 74 | 14.5s | 5.4s | 0.37 |
| 中文 Piper | 339 | 50.7s | 19.5s | 0.39 |
| 英文 Piper | 110 | 5.6s | 2.4s | 0.43 |
| 中文 Matcha | 74 | 14.7s | 5.6s | 0.38 |

并发：4 个请求同时打进来，串行执行，总 3.9s（每个约 0.95s）；`queue_size=1` 时第 3 个请求正确返回 `429`。

注意短句：2 秒左右的音频 RTF 会升到 0.45–0.47（固定开销占比大），长文本才接近 0.37。

## 与原始方案的关键差异

1. **sherpa-onnx 必须 pin 在 v1.13.8**。v2.0.0 移除了 espeak-ng / piper-phonemize，
   Piper 中英文模型（espeak 音素）在 v2.x 上无法直接做文本→音素，必须先自行预音素化。
   v1.13.8 是最后一个 `data_dir`（espeak-ng-data）可用的版本，Piper/Matcha 都能开箱即用。
2. **用薄 cgo 而不是官方 Go binding**。官方 `sherpa-onnx-go-linux` 覆盖了 TTS，但仓库
   自带的 .so 已对齐 v2.x，且会把三套架构的库拉进模块。本项目只包 `Create/Generate/
   SampleRate/NumSpeakers/Destroy`，约 200 行，库路径由 Makefile 注入，更可控。
3. **Matcha `zh-baker` 可以当默认可选音色**：RTF 与 Piper 基本相同（0.38），韵律更好；
   代价是多占约 140MB 磁盘（模型 88MB + vocoder 52MB）。
4. **中文 Piper 依赖模型包内的 `espeak-ng-data`**（`vits-piper-zh_CN-huayan-medium.tar.bz2`
   自带），配置里 `data_dir` 指过去即可，不需要额外下载。
5. **mp3 需要 ffmpeg**。Pi 上没装 ffmpeg，`response_format=mp3` 会返回 400（并提示原因）。
   默认格式改为 `wav`（OpenAI 官方默认是 mp3，这点不一致，客户端应显式传 `wav`）。
6. **`speed` 非线性**。实测中文 `speed=2.0` 只把时长压到 0.75x，`0.5` 只拉到 1.19x；
   和模型内部的句子切分/停顿有关，别拿它当精确变速用。
7. **磁盘才是瓶颈**，不是 CPU/内存。测试机 SD 卡可用空间只有 1.4GB，装完 Go 工具链
   （250MB）、sherpa 库（75MB）、中英 Piper 模型（158MB）、Matcha（140MB）、Go 构建缓存
   （98MB）后只剩约 470MB。生产机上建议只留最终要用的模型。

## 目录结构

```
cmd/voxpi/          main：配置加载、HTTP 服务、优雅退出
internal/tts/       sherpa-onnx C API 的薄 cgo 封装（vits / matcha）
internal/audio/     float → PCM16 / WAV 编码（纯 Go，带单测）
internal/config/    voxpi.json 解析与路径校验
internal/server/    路由、鉴权、单 worker 队列、429、时间指标
third_party/        sherpa-onnx v1.13.8 的 c-api.h
scripts/            下载预编译库与模型
deploy/             systemd unit
```

## 快速开始

依赖：`gcc`、`make`、`curl`、`bzip2`、Go 1.22+（树莓派上直接装官方 arm64 包即可）。

```bash
# 1. 预编译库（按当前架构自动选资产，默认 v1.13.8）
make libs

# 2. 模型（不传名字则下中英两个 Piper）
make models
# 只下中文：
./scripts/fetch-models.sh ./models vits-piper-zh_CN-huayan-medium

# 3. 配置
cp voxpi.example.json voxpi.json    # 按需增删 models、改 listen

# 4. 构建 + 运行
make build
./bin/voxpi -config voxpi.json
```

树莓派上从零装 Go（无 sudo）：

```bash
curl -fL -o /tmp/go.tgz https://go.dev/dl/go1.27.3.linux-arm64.tar.gz
mkdir -p ~/opt && tar xzf /tmp/go.tgz -C ~/opt --strip-components=1
export PATH="$HOME/opt/bin:$PATH"
```

## API

请求与 OpenAI `/v1/audio/speech` 对齐：

```bash
curl -X POST http://127.0.0.1:8000/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"model":"piper-zh-huayan","input":"你好，欢迎使用 VoxPi。","response_format":"wav","speed":1.0}' \
  -o out.wav
```

| 字段 | 说明 |
|---|---|
| `model` | 配置里 `models` 的 key；只加载了一个模型时可省略 |
| `voice` | 找不到 `model` 时按 key 解析，方便接只传 voice 的客户端 |
| `input` | 必填，默认上限 2000 字符（`max_input_chars`） |
| `response_format` | `wav`（默认）/ `pcm`(16-bit LE 单声道) / `mp3`（需 ffmpeg） |
| `speed` | 0.25–4.0，默认 1.0；映射到 sherpa 的 length_scale |

响应头带实测指标，方便压测：`X-VoxPi-Model`、`X-VoxPi-Gen-Ms`、`X-VoxPi-Audio-Ms`、
`X-VoxPi-Rtf`。

其他端点：

- `GET /v1/models`：OpenAI 风格的模型列表（附 `sample_rate`、`speakers`）
- `GET /healthz`：免鉴权，返回队列深度、采样率、`temp_c`、`rss_mb`
- 忙时（队列满）返回 `429` + `Retry-After`
- 配置了 `api_key` 或环境变量 `VOXPI_API_KEY` 时，除 `/healthz` 外都要 `Authorization: Bearer <key>`

## 并发模型

模型只加载一次常驻内存，`queue_size` 个作业槽 + 1 个 worker 串行推理。goroutine 不会让
单次合成更快，worker 数 >1 只会争抢 4 个 CPU 核，所以故意保持单 worker。

## 部署

```bash
sudo mkdir -p /opt/voxpi
sudo cp bin/voxpi /opt/voxpi/
sudo cp -a vendor/sherpa-onnx/lib /opt/voxpi/lib
sudo cp -a models /opt/voxpi/models
sudo cp voxpi.json /opt/voxpi/
sudo cp deploy/voxpi.service /etc/systemd/system/
sudo systemctl enable --now voxpi
```

二进制带 `-Wl,-rpath,$ORIGIN/lib`，库放同级 `lib/` 即可；service 里另外设了
`LD_LIBRARY_PATH` 兜底。服务默认只监听 `127.0.0.1:8000`，公网入口交给 Caddy 或
Cloudflare Tunnel，不要直接暴露树莓派。

## 已知限制 / 后续

- 无流式输出：整段合成完再返回。长文本（>300 字）首字节延迟 ≈ 生成耗时。
- RSS 会随长文本上涨（339 字后到 ~390MB），onnxruntime 的 arena 不会立刻还给系统；
  4GB 机器上够用，但别把 `queue_size` 和 `max_input_chars` 同时调大。
- 无 `/metrics`（Prometheus），目前靠响应头和 `/healthz`。
- 无音素/多说话人控制；多说话人模型可用 `sid`，但接口暂未暴露。
- 想换 Matcha：把 `engine` 设为 `matcha`，填 `model`(声学模型)、`vocoder`、`lexicon`、
  `tokens`，参考 `voxpi.matcha.example.json`。实测 `zh-baker` RTF 0.38，可直接替换默认音色。
- 如果哪天要升 sherpa-onnx v2.x，需要先把文本预音素化（`piper-phonemize-go` 或自建
  拼音词典），并改用 `phoneme_codepoints` / `tokens` 传参。
