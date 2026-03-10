[![GitHub Workflow Status (branch)](https://img.shields.io/github/actions/workflow/status/yylego/restyoops/release.yml?branch=main&label=BUILD)](https://github.com/yylego/restyoops/actions/workflows/release.yml?query=branch%3Amain)
[![GoDoc](https://pkg.go.dev/badge/github.com/yylego/restyoops)](https://pkg.go.dev/github.com/yylego/restyoops)
[![Coverage Status](https://img.shields.io/coveralls/github/yylego/restyoops/main.svg)](https://coveralls.io/github/yylego/restyoops?branch=main)
[![Supported Go Versions](https://img.shields.io/badge/Go-1.25+-lightgrey.svg)](https://go.dev/)
[![GitHub Release](https://img.shields.io/github/release/yylego/restyoops.svg)](https://github.com/yylego/restyoops/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/yylego/restyoops)](https://goreportcard.com/report/github.com/yylego/restyoops)

# restyoops

Oops! 检查 restyv2 响应是否可重试。

结构化 HTTP 操作故障分类，带有可重试语义，适用于 go-resty/resty/v2。

---

<!-- TEMPLATE (ZH) BEGIN: LANGUAGE NAVIGATION -->

## 英文文档

[ENGLISH README](README.md)

<!-- TEMPLATE (ZH) END: LANGUAGE NAVIGATION -->

## 核心特性

🎯 **故障分类**: 将 HTTP 响应结果分类为可操作的类别
⚡ **可重试检测**: 使用合理的默认值判断操作是否可重试
🔄 **可配置设置**: 按状态码和类型自定义设置
🔍 **内容检查**: 自定义内容检查，处理特殊情况（验证码、WAF、业务码）
⏱️ **等待时间**: 重试前的建议等待时间

## 安装

```bash
go get github.com/yylego/restyoops
```

## 快速开始

```go
package main

import (
    "fmt"
    "github.com/go-resty/resty/v2"
    "github.com/yylego/restyoops"
)

func main() {
    client := resty.New()

    detective := restyoops.NewDetective(restyoops.NewConfig())
    resp, oops := detective.Detect(client.R().Get("https://api.example.com/data"))

    if oops != nil {
        fmt.Printf("类型: %s, 可重试: %v\n", oops.Kind, oops.Retryable)
        if oops.IsRetryable() {
            fmt.Printf("重试前等待: %v\n", oops.WaitTime)
        }
        return
    }

    fmt.Println("请求成功！")
    fmt.Println("响应:", string(resp.Body()))
}
```

## Detective（推荐）

`Detective` 封装配置并提供便捷的 API，可直接接受 resty 的返回值：

```go
type OopsIssue = Oops

detective := restyoops.NewDetective(restyoops.NewConfig())
resp, oops := detective.Detect(client.R().Get(url))  // 无需: resp, err := ...; 再 Detect(..., resp, err)
if oops != nil {
    // 处理问题
    return
}
// 成功
data := resp.Body()
```

**优势**: 避免先 `resp, err := client.R().Get(url)` 再 `Detect(cfg, resp, err)` 的模式。

## Kind 分类

| Kind           | 描述                              | 默认可重试 |
| -------------- | --------------------------------- | ---------- |
| `KindNetwork`  | 网络问题（超时、DNS、TCP、TLS）   | true       |
| `KindHttp`     | HTTP 4xx/5xx 状态码               | 取决于状态 |
| `KindParse`    | 响应解析失败                      | false      |
| `KindBlock`    | 请求被阻止（验证码、WAF）         | false      |
| `KindBusiness` | 业务逻辑问题（HTTP 200，code!=0） | false      |
| `KindUnknown`  | 未分类的问题                      | false      |

**注意**: 当成功时返回 `nil`（没有 oops 表示没问题）。

## 默认 HTTP 状态码可重试

| 状态码             | 可重试 |
| ------------------ | ------ |
| 408 请求超时       | true   |
| 429 请求过多       | true   |
| 500 服务端内部问题 | true   |
| 502 网关问题       | true   |
| 503 服务不可用     | true   |
| 504 网关超时       | true   |
| 400 请求问题       | false  |
| 401 未授权         | false  |
| 403 禁止访问       | false  |
| 404 未找到         | false  |
| 409 冲突           | false  |
| 422 无法处理的实体 | false  |
| 其他 5xx           | true   |
| 其他 4xx           | false  |

## 自定义配置

### 配置优先级

检测时，配置按以下顺序应用（从高到低）：

1. **ContentChecks** - 自定义内容检查函数（最先检查）
2. **StatusOptions** - 按状态码的配置
3. **KindOptions** - 按类型的配置
4. **Default** - 内置默认值

如果高优先级配置匹配，则跳过低优先级的配置。

### 自定义状态码设置

```go
cfg := restyoops.NewConfig().
    WithStatusRetryable(403, true, 5*time.Second).  // 使 403 可重试
    WithStatusRetryable(500, false, 0)              // 使 500 不可重试

oops := restyoops.Detect(cfg, resp, err)
```

### 自定义 Kind 设置

```go
cfg := restyoops.NewConfig().
    WithKindRetryable(restyoops.KindNetwork, true, 10*time.Second)

oops := restyoops.Detect(cfg, resp, err)
```

### 自定义内容检查

```go
cfg := restyoops.NewConfig().
    WithContentCheck(200, func(contentType string, content []byte) *restyoops.Oops {
        if bytes.Contains(content, []byte("captcha")) {
            return restyoops.NewOops(restyoops.KindBlock, 200, errors.New("CAPTCHA DETECTED"), true).WithWaitTime(5*time.Second)
        }
        return nil // 通过，继续默认检测
    })

oops := restyoops.Detect(cfg, resp, err)
```

### 设置默认等待时间

```go
cfg := restyoops.NewConfig().
    WithDefaultWait(2 * time.Second)

oops := restyoops.Detect(cfg, resp, err)
```

## Oops 结构体

```go
type Oops struct {
    Kind        Kind          // 分类
    StatusCode  int           // HTTP 状态码
    ContentType string        // 响应 Content-Type
    Cause       error         // 被包装的原因（不为空）
    Retryable   bool          // 是否可通过重试解决
    WaitTime    time.Duration // 建议等待时间
}
```

## Detect 函数（基础 API）

```go
func Detect(cfg *Config, resp *resty.Response, respCause error) *Oops
```

用法:

```go
resp, err := client.R().Get(url)
oops := restyoops.Detect(restyoops.NewConfig(), resp, err)
```

---

<!-- TEMPLATE (ZH) BEGIN: STANDARD PROJECT FOOTER -->
<!-- VERSION 2025-11-25 03:52:28.131064 +0000 UTC -->

## 📄 许可证类型

MIT 许可证 - 详见 [LICENSE](LICENSE)。

---

## 💬 联系与反馈

非常欢迎贡献代码！报告 BUG、建议功能、贡献代码：

- 🐛 **问题报告？** 在 GitHub 上提交问题并附上重现步骤
- 💡 **新颖思路？** 创建 issue 讨论
- 📖 **文档疑惑？** 报告问题，帮助我们完善文档
- 🚀 **需要功能？** 分享使用场景，帮助理解需求
- ⚡ **性能瓶颈？** 报告慢操作，协助解决性能问题
- 🔧 **配置困扰？** 询问复杂设置的相关问题
- 📢 **关注进展？** 关注仓库以获取新版本和功能
- 🌟 **成功案例？** 分享这个包如何改善工作流程
- 💬 **反馈意见？** 欢迎提出建议和意见

---

## 🔧 代码贡献

新代码贡献，请遵循此流程：

1. **Fork**：在 GitHub 上 Fork 仓库（使用网页界面）
2. **克隆**：克隆 Fork 的项目（`git clone https://github.com/yourname/repo-name.git`）
3. **导航**：进入克隆的项目（`cd repo-name`）
4. **分支**：创建功能分支（`git checkout -b feature/xxx`）
5. **编码**：实现您的更改并编写全面的测试
6. **测试**：（Golang 项目）确保测试通过（`go test ./...`）并遵循 Go 代码风格约定
7. **文档**：面向用户的更改需要更新文档
8. **暂存**：暂存更改（`git add .`）
9. **提交**：提交更改（`git commit -m "Add feature xxx"`）确保向后兼容的代码
10. **推送**：推送到分支（`git push origin feature/xxx`）
11. **PR**：在 GitHub 上打开 Merge Request（在 GitHub 网页上）并提供详细描述

请确保测试通过并包含相关的文档更新。

---

## 🌟 项目支持

非常欢迎通过提交 Merge Request 和报告问题来贡献此项目。

**项目支持：**

- ⭐ **给予星标**如果项目对您有帮助
- 🤝 **分享项目**给团队成员和（golang）编程朋友
- 📝 **撰写博客**关于开发工具和工作流程 - 我们提供写作支持
- 🌟 **加入生态** - 致力于支持开源和（golang）开发场景

**祝你用这个包编程愉快！** 🎉🎉🎉

<!-- TEMPLATE (ZH) END: STANDARD PROJECT FOOTER -->

---

## GitHub 标星点赞

[![标星点赞](https://starchart.cc/yylego/restyoops.svg?variant=adaptive)](https://starchart.cc/yylego/restyoops)
