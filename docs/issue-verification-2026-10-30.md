# SCTP issue 真实性复核

复核日期：2026-10-30。

**结论：#1、#2、#4–#11 的十项现象均重新复现，未发现应按测试误报关闭的条目。** 其中七项是功能或数据正确性缺陷，#7、#10、#11 是非法或不完整输入下的参数校验与健壮性问题，不能描述为正常连接流程必然出错。#3 是发布规划，不计入 bug 数量。

所有问题仍未修复。实际修复进度统一见 [v0.7.2 发布规划 #3](https://github.com/skill215/sctp/issues/3)。

## 复核方法与范围

- 使用正式标签 `0.7.1` 的独立源码归档，提交为 `e864ee06b6c5b0c77661c4715814a75576c23237`。
- 环境为 Linux/amd64，内核 `6.6.114.1-microsoft-standard-WSL2`。
- Go 1.26.10 和 Go 1.27.2 分别执行全部已登记复现用例五轮，启用 race detector。
- 两套版本分别执行六组对照测试三轮，核验正常收发、原始空载荷 sendmsg、reset 是否被消耗、地址是否共享、公开通知回调和实际绑定地址。
- 核对 Go 的 Reader 语义、Go 标准库地址校验行为，以及 Linux SCTP UAPI 和 EOF 的适用 socket 类型。

失败用例的原始断言针对修复后应有的行为；在未修复的 0.7.1 上仍失败。对照测试用于排除错误归因，有些断言刻意描述未修复行为，不能原样充当修复后的验收测试。

## 逐项结论

| Issue | 分类 | 再次确认的事实与对照 |
| --- | --- | --- |
| [#1](https://github.com/skill215/sctp/issues/1) | 功能缺陷，P1 | reset 未被消费时 Read 能取得终止错误；Write 消费 reset 后，Read 等待超过1秒仍不返回，Close 才唤醒。断连前 GetStatus 为 ESTABLISHED，断连后查询返回 EINVAL，排除了单纯的250ms调度延迟疑虑。 |
| [#2](https://github.com/skill215/sctp/issues/2) | 并发缺陷，P2 | 共用同一通配地址触发 race，并改写输入的 IPAddrs；每个调用分别持有地址对象的对照无 race。动态证据针对 Listen，Dial 的同类原地修改路径仍需在修复时补回归。 |
| [#4](https://github.com/skill215/sctp/issues/4) | 数据正确性缺陷，P1 | Read(nil) 返回1并丢失首字节；32字节 wrapper 缓冲区返回33。正常容量的 payload 与 metadata 收发对照通过。 |
| [#5](https://github.com/skill215/sctp/issues/5) | 数据正确性缺陷，P1 | 库的空 payload 加 metadata 发送注入0x00；原始零载荷 sendmsg 返回 EINVAL，不向对端注入字节，随后正常发送仍成功。 |
| [#6](https://github.com/skill215/sctp/issues/6) | UAPI 常量错误，P2 | 在适用 EOF 的 one-to-many socket 上，原始 sendmsg 使用0x10被拒绝，使用正确的0x200后对端收到 EOF。测试避开了 Go SendmsgN 补字节行为。 |
| [#7](https://github.com/skill215/sctp/issues/7) | 参数校验问题，P2 | 高层 Listen 接受65536并实际绑定其他端口，Dial 接受65536+p并实际连接p；Go ListenTCP 对非法端口返回错误。 |
| [#8](https://github.com/skill215/sctp/issues/8) | 功能缺陷，P2 | 使用公开的 SocketConfig.NotificationHandler 配置：未包装时收到一次 SHUTDOWN 回调，包装后回调为零且直接得到 EOF，证明影响实际通知投递。 |
| [#9](https://github.com/skill215/sctp/issues/9) | 数据正确性缺陷，P2 | 无 metadata 的 SENDER_DRY 通知导致返回头部保留0xa5旧内容；正常 payload 的 wrapper 对照能正确覆盖头部并返回 stream=1、PPID=42。 |
| [#10](https://github.com/skill215/sctp/issues/10) | 输入健壮性问题，P2 | nil raddr 导致 panic；Go DialTCP 对同类缺失地址返回 missing address。有效 raddr 配合 nil laddr 的自动绑定正常。 |
| [#11](https://github.com/skill215/sctp/issues/11) | 输入校验与语义风险，P2 | 127.0.0.1/:0 被解析成通配地址，实际 Getsockname 为0.0.0.0；合法单地址127.0.0.1:0仍绑定127.0.0.1，单独:0的合法通配行为也正常。 |

## 触发范围与记录修正

### 读取长度有明确接口约束

Go Reader 要求返回计数不超过缓冲区长度，并明确零长度缓冲区应返回零计数。#4 的计数与丢字节现象违反该约束，不能解释为调用方提供空缓冲区后的正常行为。[Go Reader 文档](https://pkg.go.dev/io#Reader)

### 空消息与关闭标志需要分别验证

#5 不要求内核必须支持空 SCTP 用户消息；拒绝空消息是可接受的行为，静默造出一个 payload 字节才是缺陷。

#6 核对的是 Linux 导出的常量，且动态验证使用 one-to-many socket。它不表示所有 SOCK_STREAM 发送都能用 SCTP_EOF，也不表示 SCTPConn.Close 普遍无法关闭。RFC 6458 说明了 one-to-many 的零数据 EOF 关闭方式；Linux UAPI 将 SCTP_EOF 定义为 MSG_FIN。[RFC 6458 第3.3节](https://www.rfc-editor.org/rfc/rfc6458.html#section-3.3)、[Linux v6.6 UAPI](https://github.com/torvalds/linux/blob/v6.6/include/uapi/linux/sctp.h#L310-L319)

### 三项参数问题不扩大为正常 I/O 故障

- #7 针对高层网络入口缺少端口范围校验。低层转换函数发生整数截断本身不足以证明 bug；本条的依据是实际监听或连接目标改变，却没有返回错误。
- #10 针对缺失远端地址的输入健壮性。它不意味着有效远端地址也会 panic，也没有据此认定可由远端流量直接触发的安全漏洞。Go 标准库的对照是明确拒绝 nil raddr。[Go 1.26.10 DialTCP](https://github.com/golang/go/blob/go1.26.10/src/net/tcpsock.go#L317-L329)
- #11 针对不完整多地址输入的处理，不声称合法多地址输入普遍失败。纯解析用例不要求内核 SCTP 模块或 socket 权限；本轮额外的实际绑定范围对照才需要这些条件。原 issue 的通用环境说明应据此收窄。

### 未重复登记相同根因

#4 的普通 Read 和 header-only wrapped Read 是同一缺陷的两个入口。#4 与 #5 都涉及 Go 系统调用的临时字节，但方向、丢失或注入数据的行为及修复边界不同。#8 是订阅配置副作用，#9 是无 metadata 时的输出完整性问题；它们可能互相暴露，但可分别验证。

## 可复现测试

- [首轮两个缺陷](repro/0.7.1-regressions_test.go.txt)。
- [后续八项缺陷](repro/0.7.1-additional-regressions_test.go.txt)。
- [本轮真实性对照](repro/0.7.1-issue-confirmation-controls_test.go.txt)。

在仓库根目录准备独立发布版源码：

```sh
sctp_verify_dir="$(mktemp -d)"
git archive 0.7.1 | tar -x -C "$sctp_verify_dir"
cp docs/repro/0.7.1-regressions_test.go.txt "$sctp_verify_dir/release_audit_linux_test.go"
cp docs/repro/0.7.1-additional-regressions_test.go.txt "$sctp_verify_dir/additional_regressions_test.go"
cp docs/repro/0.7.1-issue-confirmation-controls_test.go.txt "$sctp_verify_dir/confirmation_controls_linux_test.go"
```

对未修复版本，对照测试应通过，缺陷断言应失败。两套 Go 工具链分别执行：

```sh
(
  cd "$sctp_verify_dir"
  go test -race -run '^TestConfirm' -v -count=3 -timeout=60s .
  go test -race -run '^Test(ReleaseAudit|Review)' -v -count=5 -timeout=60s .
)
```

上述结论只涵盖所列基线、环境和触发条件；issue 真实性确认不等于完整库已无其他缺陷，也不等于修复已完成。
