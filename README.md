[![GitHub Workflow Status (branch)](https://img.shields.io/github/actions/workflow/status/yylego/restyoops/release.yml?branch=main&label=BUILD)](https://github.com/yylego/restyoops/actions/workflows/release.yml?query=branch%3Amain)
[![GoDoc](https://pkg.go.dev/badge/github.com/yylego/restyoops)](https://pkg.go.dev/github.com/yylego/restyoops)
[![Coverage Status](https://img.shields.io/coveralls/github/yylego/restyoops/main.svg)](https://coveralls.io/github/yylego/restyoops?branch=main)
[![Supported Go Versions](https://img.shields.io/badge/Go-1.25+-lightgrey.svg)](https://go.dev/)
[![GitHub Release](https://img.shields.io/github/release/yylego/restyoops.svg)](https://github.com/yylego/restyoops/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/yylego/restyoops)](https://goreportcard.com/report/github.com/yylego/restyoops)

# restyoops

Oops! Decide whether a failed resty request is worth sending again.

Retry judgement for `go-resty/resty/v2`: whether to send it again, how long to hold off, and what kind of fault it was.

---

<!-- TEMPLATE (EN) BEGIN: LANGUAGE NAVIGATION -->

## CHINESE README

[中文说明](README.zh.md)

<!-- TEMPLATE (EN) CLOSE: LANGUAGE NAVIGATION -->

## The Problem

resty already owns a retry loop, an exponential backoff and the attempt counting. What it leaves to its users is the judgement, and left alone that judgement goes wrong in ways that are easy to miss.

Each row below comes from a test in this repo, run against resty v2.17.2:

| What happens | resty on its own |
| ------------ | ---------------- |
| `SetRetryCount(3)` and the peer answers `500` | The peer is reached **once**. Status codes are never repeated |
| The connection is refused | Repeated 4 times ✅ |
| `AddRetryAfterErrorCondition()` added, then the connection is refused | Repeated **0** times. resty's own helper silences retries on transport faults |
| Same helper, and the peer answers `404` | Reached **4** times, hammering a peer over a resource that stays absent |
| The peer answers `429` with `Retry-After: 120` | Goes again after **102ms**. The header is never read |
| An untrusted certificate | Repeated 4 times, though no certificate becomes trusted through repetition |
| `POST` answered with `500` | Repeated, though the order behind it may already have been placed |

The reason the third row happens: resty starts each round assuming a transport fault repeats, then lets **every** condition overwrite that assumption. One condition answering "no" silences it entirely.

This package supplies the judgement, installs it in one call, and reports each fault as an error carrying its classification.

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

`Setup` installs the policy onto the client once. Every call site stays ordinary Go: one `if err != nil` catches transport faults and fault status codes alike, and `errors.As` reaches the classification when it is wanted.

## What The Defaults Decide

**Transport faults**

| Fault | Kind | Send again | Why |
| ----- | ---- | ---------- | --- |
| Connection refused, reset, unreachable | `KindNetwork` | yes | The peer may come back |
| Timeout, deadline exceeded | `KindNetwork` | yes | Time running out often clears up |
| Context canceled | `KindCanceled` | no | The context is dead, another attempt dies at once |
| Name does not resolve | `KindRequest` | no | Absent names stay absent |
| Unsupported scheme, no host, too many redirects | `KindRequest` | no | The request itself can never get past this |
| Untrusted certificate, denied handshake | `KindTLS` | no | Trust does not arrive through repetition |
| Anything unrecognized | `KindUnknown` | no | Better than hammering a peer over an unknown shape |

**Status codes**

| Status | Kind | Send again |
| ------ | ---- | ---------- |
| 408, 425 | `KindClient` | yes |
| 429 | `KindThrottle` | yes, holding off as `Retry-After` asks |
| 500, 502, 503, 504, other 5xx | `KindUpstream` | yes |
| 501, 505 | `KindUpstream` | no |
| 400, 401, 403, 404, other 4xx | `KindClient` | no |
| below 400 | — | not a fault, `Detect` returns nil |

**Repeats that are not safe.** A method outside `GET HEAD OPTIONS TRACE PUT DELETE` may already have taken effect, and no status code can tell whether it did. Such a method is not repeated, with one exception: `429` means the peer turned the request away without acting on it, so repeating it is safe. `WithRepeatMethods` takes that decision back.

**Kinds a custom check can report.** `KindBlock` (captcha, WAF, login redirect), `KindBusiness` (a fault code inside a 200), `KindParse`. The built-in detection never produces these, since recognizing them needs knowledge about the peer. `Kind` is an open type, so a kind of your own works the same way.

## Configuration

```go
client := restyoops.Setup(resty.New(),
    restyoops.WithAttempts(3),                          // attempts after the first
    restyoops.WithWaitTime(100*time.Millisecond, 30*time.Second),
    restyoops.WithStatus(403, restyoops.Again(5*time.Second)),
    restyoops.WithKind(restyoops.KindUpstream, restyoops.Abort()),
    restyoops.WithRepeatMethods("GET", "HEAD", "POST"),
    restyoops.WithErrorOnFault(false),                  // keep resty's shape: a 500 arrives with a nil error
)
```

`Again()` hands the wait to the backoff, which grows it with each attempt. `Again(d)` states the wait, which then stays flat. `Abort()` refuses another attempt. A stated wait is kept as stated, including a stated zero — though resty raises any wait up to the floor set through `WithWaitTime`, which is a limit this package cannot lift.

Precedence: a custom check outranks everything, then a rule pinned to one status code, then a rule covering a kind, then the built-in decision.

Two boundaries worth stating, so nothing configured turns out to be quietly inert. `WithStatus` and `WithKind` refine a verdict rather than create one: they reach status codes already counted as faults, meaning 400 and above. And they reach the built-in detection's verdicts, not what a check reports — a check saw the response itself, so its verdict stands as given. Turning a 200 or a 302 into a fault is what `WithCheck` is there for.

## Custom Checks

A check sees the whole response, so headers, payload and the request behind it are all reachable. Returning nil hands the decision back.

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

## Classifying Without Taking Over

`Detect` answers what a finished call ran into, leaving the retrying alone. It reads what a resty call returns, and returns nil when nothing went wrong.

```go
oops := restyoops.Detect(resty.New().R().Get(url))
if oops != nil {
    fmt.Println(oops.Kind, oops.Retryable, oops.WaitTime)
}
```

Calling it again on its own output returns the same verdict, so it is safe anywhere in the flow.

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
    WaitTime    time.Duration // 0 means the backoff decides
    Cause       error         // nil when the status code says it much
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

## GitHub Stars

[![Stargazers](https://starchart.cc/yylego/restyoops.svg?variant=adaptive)](https://starchart.cc/yylego/restyoops)
