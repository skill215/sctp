# SCTP 0.7.1 发布审计

审计日期：2026-10-30。

**结论：0.7.1 存在两个已复现的缺陷，不能认定为无已知 bug。** 原有测试通过，但补充的异常断连和共享地址探测失败。下一版本应优先修复这两个发布阻塞项，见 [v0.7.2 目标](release-plan-v0.7.2.md)。

本报告记录首轮审计。后续复查另确认八类缺陷，已登记为 #4–#11；完整发布阻塞清单与复现源码入口见 [v0.7.2 目标](release-plan-v0.7.2.md)及 [GitHub 发布规划 #3](https://github.com/skill215/sctp/issues/3)。

## 审计基线

- 正式 release：[0.7.1](https://github.com/skill215/sctp/releases/tag/0.7.1)。
- 标签对应提交：[e864ee0](https://github.com/skill215/sctp/commit/e864ee06b6c5b0c77661c4715814a75576c23237)。
- 从标签归档到独立目录执行测试；主分支 `bd2fa27` 与该标签的 Go 源码及 go.mod 没有差异。
- 本轮环境：Linux/amd64，内核 `6.6.114.1-microsoft-standard-WSL2`，Go 1.26.10 和 Go 1.27.2。

审计结果反映上述环境与覆盖范围，不是对所有输入、内核和部署方式的无缺陷保证。

## 验证结果

| 检查 | 结果 | 范围 |
| --- | --- | --- |
| 原有完整测试，race、随机顺序、十轮 | 两套 Go 版本通过 | Go 1.26.10：71.618s；Go 1.27.2：65.070s |
| go vet | 两套 Go 版本通过 | 当前发布版源码 |
| 严格 checkptr 与低 GOGC | 通过，10.244s | Go 1.26.10；关键 I/O、通知、生命周期、ABI 和编码测试三轮 |
| 接口名与数字索引冲突 | 实际执行三轮通过 | Go 1.26.10；启用 `SCTP_TEST_REQUIRE_NETNS=1`，拒绝跳过 |
| 六种架构测试二进制交叉编译 | 全部通过 | ARM、ARM64、MIPS、MIPS64、PPC64、s390x；Go 1.26.10；CGO 关闭 |
| 发布时的 GitHub Actions | 全部成功，已重新核对记录 | Go 1.26/1.27、六种架构 QEMU 检查及 publish；属于此前发布验证记录 |
| 新增 peer ABORT 探测 | 两套 Go 版本均失败 | Read、SCTPRead、wrapped Read，各三轮 |
| 新增共享通配地址探测 | 两套 Go 版本均失败 | 地址被修改，并出现 data race；各三轮 |

此前发布验证记录见 [release workflow](https://github.com/skill215/sctp/actions/runs/40845986390)。本轮架构验证只重新执行交叉编译，没有重新运行 QEMU；QEMU 中的 ABI/编码检查也不能替代完整 SCTP 关联的原生架构测试。

原有测试执行命令：

```sh
go test -race -shuffle=on -count=10 -timeout=180s ./...
go vet ./...
```

严格指针与低 GOGC 检查：

```sh
GOGC=20 go test -race -gcflags=all=-d=checkptr=2 \
  -run '^Test(NativeCodec.*|ParseSndRcvInfo.*|.*SocketTimeout.*|.*Notification.*|.*RawConn.*|.*PeelOff.*|.*Wrapped.*|.*NumericIPv6.*|.*Mixed.*|.*FD.*)$' \
  -count=3 -timeout=120s ./...
```

## P1 对端 ABORT 后读取持续等待

跟踪 issue：[#1](https://github.com/skill215/sctp/issues/1)，关联 [v0.7.2 发布规划 #3](https://github.com/skill215/sctp/issues/3)。

### 触发条件与结果

1. 建立 SCTP client/server 关联。
2. 服务端设置 `SO_LINGER={Onoff:1,Linger:0}` 后关闭，向客户端发送 ABORT。
3. 客户端一次 Write 消耗 `ECONNRESET`。
4. 客户端继续 Read、SCTPRead 或 wrapped Read，且不设置接收超时。

关联已经终止，读取应及时返回 EOF 或可识别的终止错误。实测三个读取入口在 250ms 观察窗口内均不返回，显式 Close 后才结束。该路径没有配置超时，源码中的等待可能无限持续。

新增测试的代表输出：

```text
TestReleaseAuditReadAfterPeerAbort/Read: read blocked after Write consumed ECONNRESET
TestReleaseAuditReadAfterPeerAbort/SCTPRead: read blocked after Write consumed ECONNRESET
TestReleaseAuditReadAfterPeerAbort/wrapped_Read: read blocked after Write consumed ECONNRESET
```

### 原因与修复要求

`SCTPRead` 在 `Recvmsg` 返回 `EAGAIN` 时交给 runtime poller 等待，没有确认 SCTP 关联是否仍存在。Linux 的 reset 错误可以被另一次 I/O 消耗，后续读取不一定再次取得 `ECONNRESET`。[发布版读取实现](https://github.com/skill215/sctp/blob/e864ee06b6c5b0c77661c4715814a75576c23237/sctp_linux.go#L181-L231)

修复必须区分“关联仍活跃但暂无数据”和“关联已经终止”，同时保留已缓冲数据、通知处理、超时预算和 Close 唤醒语义。临时调用方措施是在遇到 reset 等关联终止错误后及时关闭连接；接收超时只能限制等待时间。

## P2 并发监听修改共享通配地址

跟踪 issue：[#2](https://github.com/skill215/sctp/issues/2)，关联 [v0.7.2 发布规划 #3](https://github.com/skill215/sctp/issues/3)。

### 触发条件与结果

多个 goroutine 共用 `addr := &SCTPAddr{Port: 0}` 调用 `ListenSCTP("sctp4", addr)`。

实测调用会把调用方原本为空的 `IPAddrs` 修改为 IPv4 通配地址。多个调用同时读写这个切片时，race detector 报告 `listenSCTPExtConfig` 与 `ToRawSockAddrBuf` 之间的数据竞争。

```text
WARNING: DATA RACE
ListenSCTP mutated caller's wildcard address: 0.0.0.0:0
```

### 原因与修复要求

监听函数为补齐通配地址而直接向 `laddr.IPAddrs` 追加元素。拨号函数存在同类原地修改路径；本轮动态复现针对监听入口。[监听路径](https://github.com/skill215/sctp/blob/e864ee06b6c5b0c77661c4715814a75576c23237/sctp_linux.go#L323-L334)、[拨号路径](https://github.com/skill215/sctp/blob/e864ee06b6c5b0c77661c4715814a75576c23237/sctp_linux.go#L479-L490)

修复应在库内部准备地址副本，避免修改调用方的结构体、切片或共享底层存储。回归测试应覆盖 Listen 与 Dial、IPv4 与 IPv6、成功与失败路径。临时调用方措施是为每个调用分别构造地址，避免共享通配 `SCTPAddr`。

## 复现新增缺陷

[复现测试源码](repro/0.7.1-regressions_test.go.txt) 使用发布版中已有的 `lifecyclePair` 测试辅助函数。文件以 `.txt` 保存，供显式复制到发布版归档后运行；它没有纳入主分支的正常 Go 测试包。修复时应将对应断言加入正式回归测试。

在仓库根目录执行：

```sh
sctp_audit_dir="$(mktemp -d)"
git archive 0.7.1 | tar -x -C "$sctp_audit_dir"
cp docs/repro/0.7.1-regressions_test.go.txt "$sctp_audit_dir/release_audit_linux_test.go"
(
  cd "$sctp_audit_dir"
  go test -race -run '^TestReleaseAudit' -v -count=3 -timeout=30s .
)
```

需要 Linux 内核 SCTP 支持和本地 socket 权限。在未修复的 0.7.1 上，预期退出码为非零；本轮测试均确认客户端已消耗 `ECONNRESET`，而非因环境无法建立关联而失败。
