[![GitHub Workflow Status (branch)](https://img.shields.io/github/actions/workflow/status/yylego/restyoops/release.yml?branch=main&label=BUILD)](https://github.com/yylego/restyoops/actions/workflows/release.yml?query=branch%3Amain)
[![GoDoc](https://pkg.go.dev/badge/github.com/yylego/restyoops)](https://pkg.go.dev/github.com/yylego/restyoops)
[![Coverage Status](https://img.shields.io/coveralls/github/yylego/restyoops/main.svg)](https://coveralls.io/github/yylego/restyoops?branch=main)
[![Supported Go Versions](https://img.shields.io/badge/Go-1.25+-lightgrey.svg)](https://go.dev/)
[![GitHub Release](https://img.shields.io/github/release/yylego/restyoops.svg)](https://github.com/yylego/restyoops/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/yylego/restyoops)](https://goreportcard.com/report/github.com/yylego/restyoops)

# restyoops

Oops! See if restyv2 response is retryable.

Structured HTTP operation fault classification with retryable semantics, designed with go-resty/resty/v2.

---

<!-- TEMPLATE (EN) BEGIN: LANGUAGE NAVIGATION -->

## CHINESE README

[中文说明](README.zh.md)

<!-- TEMPLATE (EN) END: LANGUAGE NAVIGATION -->

## Main Features

🎯 **Fault Classification**: Categorize HTTP response outcomes into actionable categories
⚡ **Retryable Detection**: Determine if an operation is retryable with sensible defaults
🔄 **Configurable Settings**: Customize settings based on status code and kind
🔍 **Content Checks**: Custom content checks handling unique cases (captcha, WAF, business codes)
⏱️ **Wait Time**: Suggested wait duration before retrying

## Installation

```bash
go get github.com/yylego/restyoops
```

## Quick Start

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
        fmt.Printf("Kind: %s, Retryable: %v\n", oops.Kind, oops.Retryable)
        if oops.IsRetryable() {
            fmt.Printf("Wait before retrying: %v\n", oops.WaitTime)
        }
        return
    }

    fmt.Println("Request success!")
    fmt.Println("Response:", string(resp.Body()))
}
```

## Detective (Recommended)

`Detective` wraps config and provides a convenient API that accepts resty's values without intermediate steps:

```go
type OopsIssue = Oops

detective := restyoops.NewDetective(restyoops.NewConfig())
resp, oops := detective.Detect(client.R().Get(url))  // No need: resp, err := ...; then Detect(..., resp, err)
if oops != nil {
    // handle issue
    return
}
// success
data := resp.Body()
```

**Advantage**: Avoids the `resp, err := client.R().Get(url)` then `Detect(cfg, resp, err)` pattern.

## Kind Classification

| Kind           | Description                              | Default Retryable |
| -------------- | ---------------------------------------- | ----------------- |
| `KindNetwork`  | Network issues (timeout, DNS, TCP, TLS)  | true              |
| `KindHttp`     | HTTP 4xx/5xx status codes                | varies            |
| `KindParse`    | Response parsing failed                  | false             |
| `KindBlock`    | Request blocked (captcha, WAF)           | false             |
| `KindBusiness` | Business logic issue (HTTP 200, code!=0) | false             |
| `KindUnknown`  | Unclassified issues                      | false             |

**Note**: Success returns `nil` (no oops means no problem).

## Default HTTP Status Retryable

| Status Code              | Retryable |
| ------------------------ | --------- |
| 408 Request Timeout      | true      |
| 429 Too Many Requests    | true      |
| 500 Internal Server Err  | true      |
| 502 Bad Gateway          | true      |
| 503 Service Unavailable  | true      |
| 504 Gateway Timeout      | true      |
| 400 Bad Request          | false     |
| 401 Unauthorized         | false     |
| 403 Forbidden            | false     |
| 404 Not Found            | false     |
| 409 Conflict             | false     |
| 422 Unprocessable Entity | false     |
| Other 5xx                | true      |
| Other 4xx                | false     |

## Custom Configuration

### Config Precedence

When detecting, configurations are applied in the following sequence (highest to lowest):

1. **ContentChecks** - Custom content check functions (checked first)
2. **StatusOptions** - Status code specific configuration
3. **KindOptions** - Kind specific configuration
4. **Default** - Built-in default values

When a high-precedence config matches, others below it are skipped.

### Customize Status Code Settings

```go
cfg := restyoops.NewConfig().
    WithStatusRetryable(403, true, 5*time.Second).  // Make 403 retryable
    WithStatusRetryable(500, false, 0)              // Make 500 not retryable

oops := restyoops.Detect(cfg, resp, err)
```

### Customize Kind Settings

```go
cfg := restyoops.NewConfig().
    WithKindRetryable(restyoops.KindNetwork, true, 10*time.Second)

oops := restyoops.Detect(cfg, resp, err)
```

### Custom Content Check

```go
cfg := restyoops.NewConfig().
    WithContentCheck(200, func(contentType string, content []byte) *restyoops.Oops {
        if bytes.Contains(content, []byte("captcha")) {
            return restyoops.NewOops(restyoops.KindBlock, 200, errors.New("CAPTCHA DETECTED"), true).WithWaitTime(5*time.Second)
        }
        return nil // pass, continue default detection
    })

oops := restyoops.Detect(cfg, resp, err)
```

### Set Default Wait Time

```go
cfg := restyoops.NewConfig().
    WithDefaultWait(2 * time.Second)

oops := restyoops.Detect(cfg, resp, err)
```

## Oops Struct

```go
type Oops struct {
    Kind        Kind          // Classification
    StatusCode  int           // HTTP status code
    ContentType string        // Response Content-Type
    Cause       error         // Wrapped cause (never nil)
    Retryable   bool          // Can be resolved via retries
    WaitTime    time.Duration // Suggested wait time
}
```

## Detect Function (Basic API)

```go
func Detect(cfg *Config, resp *resty.Response, respCause error) *Oops
```

Usage:

```go
resp, err := client.R().Get(url)
oops := restyoops.Detect(restyoops.NewConfig(), resp, err)
```

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

<!-- TEMPLATE (EN) END: STANDARD PROJECT FOOTER -->

---

## GitHub Stars

[![Stargazers](https://starchart.cc/yylego/restyoops.svg?variant=adaptive)](https://starchart.cc/yylego/restyoops)
