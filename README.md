<!-- TEMPLATE (EN) BEGIN: BADGES -->

[![GitHub Workflow Status (branch)](https://img.shields.io/github/actions/workflow/status/yylego/restyoops/release.yml?branch=main&label=BUILD)](https://github.com/yylego/restyoops/actions/workflows/release.yml?query=branch%3Amain)
[![GoDoc](https://pkg.go.dev/badge/github.com/yylego/restyoops)](https://pkg.go.dev/github.com/yylego/restyoops)
[![Coverage Status](https://img.shields.io/coveralls/github/yylego/restyoops/main.svg)](https://coveralls.io/github/yylego/restyoops?branch=main)
[![Supported Go Versions](https://img.shields.io/badge/Go-1.26%2B-lightgrey.svg)](https://go.dev/)
[![GitHub Release](https://img.shields.io/github/release/yylego/restyoops.svg)](https://github.com/yylego/restyoops/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/yylego/restyoops)](https://goreportcard.com/report/github.com/yylego/restyoops)

<!-- TEMPLATE (EN) CLOSE: BADGES -->

# restyoops

Oops! Detect faults in Resty requests and decide on repeats.

Fault classification and repeat advice with `go-resty/resty/v2`.

---

<!-- TEMPLATE (EN) BEGIN: LANGUAGE NAVIGATION -->

## CHINESE README

[中文说明](README.zh.md)

<!-- TEMPLATE (EN) CLOSE: LANGUAGE NAVIGATION -->

## The Problem

Resty provides loops, backoff and attempt counts. This package supplies fault classification and repeat rules.

The following cases describe Resty v2.17.2's built-in decisions:

| Scenario                                                  | Resty outcome                                                |
| --------------------------------------------------------- | ------------------------------------------------------------ |
| `SetRetryCount(3)` with HTTP `500`                        | One attempt without status-based conditions                  |
| Refused connection with `SetRetryCount(3)`                | Up to 4 attempts                                             |
| `AddRetryAfterErrorCondition()` with a refused connection | No repeats from this condition                               |
| `AddRetryAfterErrorCondition()` with HTTP `404`           | Repeats are permitted                                        |
| HTTP `429` with `Retry-After: 120`                        | Requires a configured callback to respect the requested wait |
| Untrusted certificate with repeats enabled                | Default transport handling can repeat the failure            |
| POST with HTTP `500` and a matching condition             | Can repeat despite possible side effects                     |

Resty checks conditions in sequence and repeats at the first true result. With no match, it stops. A condition that checks HTTP status alone can thus miss transport faults.

Use `Detect` to inspect outcomes; use `Setup` to configure Resty's loop and response checks.

## Installation

```bash
go get github.com/yylego/restyoops
```

## Quick Start

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

    fmt.Println("success:", string(resp.Body()))
}
```

Invoke `Setup` once before using the client. It configures counts, waits and response checks. The `RetryAfter` callback enforces the verdict and wait budget even if a separate condition accepts; do not replace it. Status faults return `*Oops`; transport faults can retain Resty's errors. Use `Detect(resp, err)` to inspect both. A condition that requests a repeat on success triggers a conflict error. Resty skips `RetryAfter` without a response object, so avoid mixing conditions.

## Default Rules

**Transport faults**

| Fault                                                    | Kind           | Send again | Reason                                |
| -------------------------------------------------------- | -------------- | ---------- | ------------------------------------- |
| Connection refused, reset, unreachable                   | `KindNetwork`  | yes        | Connection faults can be transient    |
| Attempt timeout, request context not known to have ended | `KindNetwork`  | yes        | Subject to the business time budget   |
| Canceled / expired request context                       | `KindCanceled` | no         | The same context cannot make progress |
| DNS reports a missing name                               | `KindRequest`  | no         | Correct the hostname                  |
| Unsupported scheme, missing host, redirect limit         | `KindRequest`  | no         | Correct the request                   |
| Certificate validation / TLS record fault                | `KindTLS`      | no         | Inspect TLS settings                  |
| Unrecognized cause                                       | `KindUnknown`  | no         | Inspect the cause                     |

**Status codes**

| Status              | Kind           | Send again                             |
| ------------------- | -------------- | -------------------------------------- |
| 408, 425            | `KindClient`   | yes                                    |
| 429                 | `KindThrottle` | yes, holding off as `Retry-After` asks |
| 5xx except 501, 505 | `KindUpstream` | yes                                    |
| 501, 505            | `KindUpstream` | no                                     |
| Remaining 4xx       | `KindClient`   | no                                     |
| below 400           | —              | not a fault, `Detect` returns nil      |

**Request methods.** Defaults permit repeats with `GET HEAD OPTIONS TRACE PUT DELETE`. Methods outside this set do not repeat even on `429`. Use `WithRepeatMethods` to replace the set based on the endpoint's contract. Missing request metadata leaves method checks to business code.

**Custom checks.** `KindBlock` (captcha, WAF, login redirect), `KindBusiness` (a fault code inside a 200) and `KindParse` need endpoint-specific knowledge. Built-in detection does not produce them. `Kind` also accepts custom values.

## Configuration

```go
client := restyoops.Setup(resty.New(),
    restyoops.WithAttempts(3),                          // repeats following the first attempt
    restyoops.WithWaitTime(100*time.Millisecond, 30*time.Second),
    restyoops.WithStatus(403, restyoops.Again(5*time.Second)),
    restyoops.WithKind(restyoops.KindUpstream, restyoops.Abort()),
    restyoops.WithRepeatMethods("GET", "HEAD", "POST"),
    restyoops.WithFaultAsOops(false),                   // preserve Resty's nil error on HTTP faults
)
```

`Again()` uses Resty's backoff, `Again(d)` states a fixed wait, and `Abort()` stops repeats. A valid `Retry-After` can increase the fixed wait; Resty also applies the minimum wait. When a stated wait exceeds the `WithWaitTime` maximum, `Setup` stops repeats. `Detect` preserves the wait so business code can reschedule the operation.

Cancellation / an ended request context stops the operation first. Precedence then follows: existing `*Oops`, custom check, status rule, kind rule, default decision. New verdicts also respect method restrictions and endpoint waits.

`WithStatus` applies to status faults (>= 400). `WithKind` adjusts built-in classifications. Custom checks retain explicit rules and can detect faults in 2xx/3xx responses.

## Custom Checks

A check receives the response and cause. The response can be absent / incomplete. A nil result proceeds to the next check; built-in detection runs if no check reports a fault.

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

## Classification Without Automatic Repeats

`Detect` classifies an attempt without sending requests / sleeping. A nil result denotes no fault.

```go
oops := restyoops.Detect(resty.New().R().Get(endpoint))
if oops != nil {
    fmt.Println(oops.Kind, oops.Retryable, oops.WaitTime)
}
```

Use this mode with business-owned loops. Omit `Setup` and disable Resty's repeats. Business code owns counts, time budgets and cancelable waits. `Retryable` is advice, not a promise of success. `WaitTimeOf` distinguishes an absent wait from a stated zero. Without request metadata, business code must also check its context.

An existing `*Oops` is returned as is unless the request context has ended.

## Oops

```go
type Oops struct {
    Kind        Kind          // Nature of the fault
    StatusCode  int           // 0 when no response arrived
    ContentType string
    Method      string
    URL         string
    Attempt     int           // Which attempt produced it, counting from 1
    Retryable   bool
    WaitTime    time.Duration // Use WaitTimeOf to distinguish unset from zero
    Cause       error         // Can be nil with status-based faults
}
```

`Oops` is an error and unwraps to `Cause`, so `errors.Is` and `errors.As` both reach through it.

---

<!-- TEMPLATE (EN) BEGIN: STANDARD PROJECT FOOTER -->
<!-- VERSION 2025-11-25 03:52:28.131064 +0000 UTC -->

## 📄 License

MIT License - see [LICENSE](LICENSE).

---

## 💬 Contact & Feedback

Contributions are welcome! Report bugs, suggest features, and contribute code:

- 🐛 **Mistake reports?** Open an issue on GitHub with reproduction steps
- 💡 **Fresh ideas?** Create an issue to discuss
- 📖 **Documentation confusing?** Report it so we can improve
- 🚀 **Need new features?** Share the use cases to help us understand requirements
- ⚡ **Performance issue?** Help us optimize through reporting slow operations
- 🔧 **Configuration problem?** Ask questions about complex setups
- 📢 **Follow project progress?** Watch the repo to get new releases and features
- 🌟 **Success stories?** Share how this package improved the workflow
- 💬 **Feedback?** We welcome suggestions and comments

---

## 🔧 Development

New code contributions, follow this process:

1. **Fork**: Fork the repo on GitHub (using the webpage UI).
2. **Clone**: Clone the forked project (`git clone https://github.com/yourname/repo-name.git`).
3. **Navigate**: Navigate to the cloned project (`cd repo-name`)
4. **Branch**: Create a feature branch (`git checkout -b feature/xxx`).
5. **Code**: Implement the changes with comprehensive tests
6. **Testing**: (Golang project) Ensure tests pass (`go test ./...`) and follow Go code style conventions
7. **Documentation**: Update documentation to support client-facing changes
8. **Stage**: Stage changes (`git add .`)
9. **Commit**: Commit changes (`git commit -m "Add feature xxx"`) ensuring backward compatible code
10. **Push**: Push to the branch (`git push origin feature/xxx`).
11. **PR**: Open a merge request on GitHub (on the GitHub webpage) with detailed description.

Please ensure tests pass and include relevant documentation updates.

---

## 🌟 Support

Welcome to contribute to this project via submitting merge requests and reporting issues.

**Project Support:**

- ⭐ **Give GitHub stars** if this project helps you
- 🤝 **Share with teammates** and (golang) programming friends
- 📝 **Write tech blogs** about development tools and workflows - we provide content writing support
- 🌟 **Join the ecosystem** - committed to supporting open source and the (golang) development scene

**Have Fun Coding with this package!** 🎉🎉🎉

<!-- TEMPLATE (EN) CLOSE: STANDARD PROJECT FOOTER -->

---

<!-- TEMPLATE (EN) BEGIN: GITHUB STARS -->

## GitHub Stars

[![Stargazers](https://starchart.cc/yylego/restyoops.svg?variant=adaptive)](https://starchart.cc/yylego/restyoops)

<!-- TEMPLATE (EN) CLOSE: GITHUB STARS -->
