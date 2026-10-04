<!-- TEMPLATE (ZH) BEGIN: BADGES -->

[![GitHub Workflow Status (branch)](https://img.shields.io/github/actions/workflow/status/yylego/restyoops/release.yml?branch=main&label=BUILD)](https://github.com/yylego/restyoops/actions/workflows/release.yml?query=branch%3Amain)
[![GoDoc](https://pkg.go.dev/badge/github.com/yylego/restyoops)](https://pkg.go.dev/github.com/yylego/restyoops)
[![Coverage Status](https://img.shields.io/coveralls/github/yylego/restyoops/main.svg)](https://coveralls.io/github/yylego/restyoops?branch=main)
[![Supported Go Versions](https://img.shields.io/badge/Go-1.26%2B-lightgrey.svg)](https://go.dev/)
[![GitHub Release](https://img.shields.io/github/release/yylego/restyoops.svg)](https://github.com/yylego/restyoops/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/yylego/restyoops)](https://goreportcard.com/report/github.com/yylego/restyoops)

<!-- TEMPLATE (ZH) CLOSE: BADGES -->

# restyoops

Oops! 判断一次失败的 resty 请求是否值得再发一次。

给 `go-resty/resty/v2` 补上重试判断：要不要再发、发之前停多久、以及那是什么性质的故障。

---

<!-- TEMPLATE (ZH) BEGIN: LANGUAGE NAVIGATION -->

## 英文文档

[ENGLISH README](README.md)

<!-- TEMPLATE (ZH) CLOSE: LANGUAGE NAVIGATION -->

## 职责与使用场景

Resty 提供重试循环、退避算法和尝试计数。本包补充故障分类与重试规则。

以下列出 Resty v2.17.2 的内置行为：

| 场景                                                | resty 自己的行为                         |
| --------------------------------------------------- | ---------------------------------------- |
| `SetRetryCount(3)`，对端返回 `500`                  | 未配置状态码重试条件时，只请求 1 次      |
| 连接被拒绝，配置 `SetRetryCount(3)`                 | 最多请求 4 次                            |
| 加上 `AddRetryAfterErrorCondition()` 后再遇连接被拒 | 该条件不会触发重试                       |
| `AddRetryAfterErrorCondition()` 遇到 `404`          | 该条件会允许重试                         |
| 对端返回 `429` 且 `Retry-After: 120`                | 需要配置回调来遵守要求的等待时间         |
| 开启重试后遇到证书不受信任                          | 默认传输错误处理可能重复请求             |
| `POST` 收到 `500`，且有匹配的重试条件               | 可能重复请求，尽管之前的操作可能已经生效 |

第三行的成因：配置重试条件后，Resty 按顺序检查，遇到第一个 true 就重试；全部返回 false 时停止。因此，条件只判断 HTTP 状态码时，可能漏掉传输故障。

使用 `Detect` 对请求结果分类；使用 `Setup` 配置 Resty 的重试循环与响应检查。

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

`Setup` 在使用 client 前安装一次，设置重试次数、等待和响应检查。其他条件即使放行，本包仍在 `RetryAfter` 阶段检查分类和等待预算；不要再覆盖该回调。状态码故障返回 `*Oops`；传输错误可能仍是 Resty 的原始错误，统一分类可以使用 `Detect(resp, err)`。如果其他条件要求重发成功响应，本包会返回冲突错误并停止。Resty 在没有响应对象时不会调用 `RetryAfter`，因此仍不建议混用额外重试条件。

## 默认分类与重试规则

**传输层故障**

| 故障                                            | 分类           | 是否重发 | 理由                      |
| ----------------------------------------------- | -------------- | -------- | ------------------------- |
| 连接被拒、被重置、不可达                        | `KindNetwork`  | 是       | 对端可能会恢复            |
| 单次请求超时，未确认请求 context 已结束         | `KindNetwork`  | 是       | 由调用方结合总时限决定    |
| context 被取消，或响应关联的请求 context 已过期 | `KindCanceled` | 否       | 相同 context 无法继续请求 |
| DNS 确认域名不存在                              | `KindRequest`  | 否       | 检查域名                  |
| 协议不支持、缺少 host、重定向超限               | `KindRequest`  | 否       | 检查请求配置              |
| 证书校验或 TLS 记录格式错误                     | `KindTLS`      | 否       | 检查 TLS 配置             |
| 未识别的错误                                    | `KindUnknown`  | 否       | 排查错误原因              |

**状态码**

| 状态码                 | 分类           | 是否重发                              |
| ---------------------- | -------------- | ------------------------------------- |
| 408、425               | `KindClient`   | 是                                    |
| 429                    | `KindThrottle` | 是，并按 `Retry-After` 要求的时长等待 |
| 除 501、505 之外的 5xx | `KindUpstream` | 是                                    |
| 501、505               | `KindUpstream` | 否                                    |
| 其余 4xx               | `KindClient`   | 否                                    |
| 小于 400               | —              | 不是故障，`Detect` 返回 nil           |

**请求方法。** 默认允许重试 `GET HEAD OPTIONS TRACE PUT DELETE`。其他方法即使收到 `429` 也不会自动重试；确认接口允许重复执行后，可用 `WithRepeatMethods` 替换允许的方法集合。没有请求信息时，分类结果无法替调用方判断接口是否允许重复执行。

**自定义检查可以报出的分类。** `KindBlock`（验证码、WAF、登录跳转）、`KindBusiness`（200 之下的业务失败码）、`KindParse`。内置检测不会产出这些，因为识别它们需要关于对端的专门知识。`Kind` 是开放类型，自己定义一个分类同样能用。

## 配置

```go
client := restyoops.Setup(resty.New(),
    restyoops.WithAttempts(3),                          // 首次之后再试几次
    restyoops.WithWaitTime(100*time.Millisecond, 30*time.Second),
    restyoops.WithStatus(403, restyoops.Again(5*time.Second)),
    restyoops.WithKind(restyoops.KindUpstream, restyoops.Abort()),
    restyoops.WithRepeatMethods("GET", "HEAD", "POST"),
    restyoops.WithFaultAsOops(false),                   // 保持 Resty 形态：500 伴随 nil 的 error 返回
)
```

`Again()` 使用 Resty 的退避算法，`Again(d)` 给出固定等待，`Abort()` 停止重试。有效的 `Retry-After` 与固定等待取较大值，Resty 仍会应用等待下限。明确要求的等待超出 `WithWaitTime` 上限时，`Setup` 停止自动重试，不截短等待；`Detect` 保留原始建议，由业务决定是否延后处理。

请求取消或 context 结束时直接停止。其他情况依次采用已有的 `*Oops`、自定义检查、单个状态码规则、分类规则、内置判断。新分类还会应用请求方法限制和对端等待要求。

`WithStatus` 调整状态码故障（400 及以上），`WithKind` 调整内置分类。自定义检查保留自己的规则，也可以识别 2xx/3xx 响应中的故障。

## 自定义检查

检查函数接收响应和错误原因。响应可能为 nil 或不完整，读取前应检查对应字段。返回 nil 时继续后续检查；均未报告故障时使用内置检测。

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
oops := restyoops.Detect(resty.New().R().Get(endpoint))
if oops != nil {
    fmt.Println(oops.Kind, oops.Retryable, oops.WaitTime)
}
```

业务已有重试循环时使用此模式，不再调用 `Setup`，并确认 Resty 客户端未开启另一层重试。业务负责次数、总时限和可取消的等待；`Retryable` 是建议，不表示重试一定成功。`WaitTimeOf` 可区分未指定等待与明确指定零等待。没有请求信息时，业务还需要检查自身 context。

已有的 `*Oops` 通常直接返回；请求 context 已结束时会改为停止结论。

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
    WaitTime    time.Duration // 用 WaitTimeOf 区分未指定与零等待
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

<!-- TEMPLATE (ZH) BEGIN: GITHUB STARS -->

## GitHub 标星点赞

[![Stargazers](https://starchart.cc/yylego/restyoops.svg?variant=adaptive)](https://starchart.cc/yylego/restyoops)

<!-- TEMPLATE (ZH) CLOSE: GITHUB STARS -->
