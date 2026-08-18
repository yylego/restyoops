[![GitHub Workflow Status (branch)](https://img.shields.io/github/actions/workflow/status/yylego/restyoops/release.yml?branch=main&label=BUILD)](https://github.com/yylego/restyoops/actions/workflows/release.yml?query=branch%3Amain)
[![GoDoc](https://pkg.go.dev/badge/github.com/yylego/restyoops)](https://pkg.go.dev/github.com/yylego/restyoops)
[![Coverage Status](https://img.shields.io/coveralls/github/yylego/restyoops/main.svg)](https://coveralls.io/github/yylego/restyoops?branch=main)
[![Supported Go Versions](https://img.shields.io/badge/Go-1.25+-lightgrey.svg)](https://go.dev/)
[![GitHub Release](https://img.shields.io/github/release/yylego/restyoops.svg)](https://github.com/yylego/restyoops/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/yylego/restyoops)](https://goreportcard.com/report/github.com/yylego/restyoops)

# restyoops

Oops! 判断一次失败的 resty 请求是否值得再发一次。

给 `go-resty/resty/v2` 补上重试判断：要不要再发、发之前停多久、以及那是什么性质的故障。

---

<!-- TEMPLATE (ZH) BEGIN: LANGUAGE NAVIGATION -->

## 英文文档

[ENGLISH README](README.md)

<!-- TEMPLATE (ZH) CLOSE: LANGUAGE NAVIGATION -->

## 这个包解决什么问题

重试循环、指数退避、尝试计数，resty 本来就有。它留给使用方的是**判断本身**，而这部分不管的话会以很难察觉的方式出错。

下面每一行都来自本仓库的测试，针对 resty v2.17.2 实测：

| 场景 | resty 自己的行为 |
| ---- | ---------------- |
| `SetRetryCount(3)`，对端返回 `500` | 对端只被访问 **1 次**。状态码从来不会被重试 |
| 连接被拒绝 | 重试 4 次 ✅ |
| 加上 `AddRetryAfterErrorCondition()` 后再遇连接被拒 | 重试 **0 次**。resty 自带的 helper 把传输层重试关掉了 |
| 同一个 helper，对端返回 `404` | 被访问 **4 次**，为一个不会出现的资源反复敲对端 |
| 对端返回 `429` 且 `Retry-After: 120` | **102ms** 后就重发。该头从来不会被读取 |
| 证书不受信任 | 重试 4 次，尽管证书不会因为重试就变得可信 |
| `POST` 收到 `500` | 会重试，尽管背后的订单可能已经下过了 |

第三行的成因：resty 每轮先假定传输故障要重试，然后让**每一个**条件去覆盖这个假定。因此只要有一个条件回答"否"，这个假定就被整个抹掉。

本包补上这部分判断，一次调用即可装好，并把每个故障作为携带分类信息的 error 报出来。

## 安装

```bash
go get github.com/yylego/restyoops
```

## 快速开始

```go
package main

import (
    "errors"
    "fmt"

    "github.com/go-resty/resty/v2"
    "github.com/yylego/restyoops"
)

func main() {
    client := restyoops.Setup(resty.New())

    resp, err := client.R().Get("https://api.example.com/data")
    if err != nil {
        var oops *restyoops.Oops
        if errors.As(err, &oops) {
            fmt.Println(oops.Kind, oops.StatusCode, oops.Attempt, oops.Retryable)
        }
        return
    }

    fmt.Println("成功:", string(resp.Body()))
}
```

`Setup` 把策略一次装到 client 上。之后每个调用点都是普通的 Go 代码：一个 `if err != nil` 同时兜住传输故障和故障状态码，需要分类信息时再用 `errors.As` 取出来。

## 默认策略判什么

**传输层故障**

| 故障 | 分类 | 是否重发 | 理由 |
| ---- | ---- | -------- | ---- |
| 连接被拒、被重置、不可达 | `KindNetwork` | 是 | 对端可能会恢复 |
| 超时、截止时间用尽 | `KindNetwork` | 是 | 时间用完这种情况常常会好转 |
| context 被取消 | `KindCanceled` | 否 | context 已经死了，再试一次会立刻再死 |
| 域名解析不出来 | `KindRequest` | 否 | 不存在的域名会一直不存在 |
| 协议不支持、缺少 host、重定向超限 | `KindRequest` | 否 | 请求本身永远迈不过去 |
| 证书不受信任、握手被拒 | `KindTLS` | 否 | 信任不会通过重复而到来 |
| 认不出的形态 | `KindUnknown` | 否 | 总好过为一个未知的东西反复敲对端 |

**状态码**

| 状态码 | 分类 | 是否重发 |
| ------ | ---- | -------- |
| 408、425 | `KindClient` | 是 |
| 429 | `KindThrottle` | 是，并按 `Retry-After` 要求的时长等待 |
| 500、502、503、504 及其它 5xx | `KindUpstream` | 是 |
| 501、505 | `KindUpstream` | 否 |
| 400、401、403、404 及其它 4xx | `KindClient` | 否 |
| 小于 400 | — | 不是故障，`Detect` 返回 nil |

**不安全的重发。** `GET HEAD OPTIONS TRACE PUT DELETE` 之外的方法可能已经生效了，而且没有任何状态码能说明它到底生效了没有。这类方法不会被重发，只有一个例外：`429` 表示对端是把请求挡回来的、根本没处理它，因此重发是安全的。想收回这个判断用 `WithRepeatMethods`。

**自定义检查可以报出的分类。** `KindBlock`（验证码、WAF、登录跳转）、`KindBusiness`（200 之下的业务失败码）、`KindParse`。内置检测不会产出这些，因为识别它们需要关于对端的专门知识。`Kind` 是开放类型，自己定义一个分类同样能用。

## 配置

```go
client := restyoops.Setup(resty.New(),
    restyoops.WithAttempts(3),                          // 首次之后再试几次
    restyoops.WithWaitTime(100*time.Millisecond, 30*time.Second),
    restyoops.WithStatus(403, restyoops.Again(5*time.Second)),
    restyoops.WithKind(restyoops.KindUpstream, restyoops.Abort()),
    restyoops.WithRepeatMethods("GET", "HEAD", "POST"),
    restyoops.WithErrorOnFault(false),                  // 保持 resty 形态：500 伴随 nil 的 error 返回
)
```

`Again()` 把等待交给退避算法，它会随尝试次数增长。`Again(d)` 明确给出等待时长，该时长不随次数变化。`Abort()` 拒绝再试。明确给出的时长会被原样保留，包括明确写下的 0 —— 但 resty 会把任何等待时长抬高到 `WithWaitTime` 设定的下限，这条限制本包无法解除。

优先级：自定义检查高于一切，其次是针对单个状态码的规则，再次是覆盖整个分类的规则，最后是内置判断。

有两条边界需要说明，免得配了半天其实悄悄没生效。`WithStatus` 和 `WithKind` 调整的是已有结论、而不是造出结论：它们只作用于已被判为故障的状态码，即 400 及以上；并且只作用于内置检测得出的结论，不作用于检查函数报出的结论——检查函数亲眼看过响应，它的结论原样作数。想把 200 或 302 变成故障，那是 `WithCheck` 的职责。

## 自定义检查

检查函数能看到整个响应，因此头、报文和背后的请求都读得到。返回 nil 表示把决定权交还回去。

```go
client := restyoops.Setup(resty.New(),
    restyoops.WithCheck(func(resp *resty.Response, cause error) *restyoops.Oops {
        if resp == nil || !bytes.Contains(resp.Body(), []byte("captcha")) {
            return nil
        }
        return restyoops.NewOops(restyoops.KindBlock,
            errors.New("captcha page served"), restyoops.Abort())
    }),
)
```

## 只分类、不接管重试

`Detect` 回答一次已完成的调用碰到了什么，而不接管重试。它直接接收 resty 调用的返回值，没出问题时返回 nil。

```go
oops := restyoops.Detect(resty.New().R().Get(url))
if oops != nil {
    fmt.Println(oops.Kind, oops.Retryable, oops.WaitTime)
}
```

把它的输出再喂给它会得到同一个结论，因此放在流程的任何位置都安全。

## Oops 结构

```go
type Oops struct {
    Kind        Kind          // 故障性质
    StatusCode  int           // 没拿到响应时为 0
    ContentType string
    Method      string
    URL         string
    Attempt     int           // 这是第几次尝试产生的，从 1 开始
    Retryable   bool
    WaitTime    time.Duration // 0 表示交给退避算法决定
    Cause       error         // 状态码已说明问题时可以为 nil
}
```

`Oops` 本身就是 error，并且会 unwrap 到 `Cause`，因此 `errors.Is` 和 `errors.As` 都能穿透它。

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

<!-- TEMPLATE (ZH) CLOSE: STANDARD PROJECT FOOTER -->

---

## GitHub 标星点赞

[![标星点赞](https://starchart.cc/yylego/restyoops.svg?variant=adaptive)](https://starchart.cc/yylego/restyoops)
