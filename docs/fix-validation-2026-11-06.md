# SCTP 修复验证记录

日期：2026-11-06。基线：v0.7.3 / e4154064。候选为本地未发布工作区；范围仅限
SCTP 仓库。此前的 review 文档及证据保留。

## 修复和回归

| Issue | 修复 | 验证重点 |
| --- | --- | --- |
| [#19](https://github.com/skill215/sctp/issues/19) | FileListener 在源文件 RawConn.Control 内复制 fd | 修改前触发 race detector；修改后并发 Close/复用只会正确导入或失败；副本与原文件各自关闭 |
| [#20](https://github.com/skill215/sctp/issues/20) | CLI 地址解析失败立即退出 | 全部无效及混合有效/无效地址都在监听前退出，实际子进程验证 |
| [#21](https://github.com/skill215/sctp/issues/21) | SetInitMsg 先校验四个整数，再转换为 uint16 | 每个字段的负值及超限值返回 EINVAL，内核读回值不变；保留 0 和 65535 |
| [#22](https://github.com/skill215/sctp/issues/22) | 示例配置与 buffer 实际读回值使用不同变量 | 三个连续客户端，显式配置和未设置两种情况都保持配置；显式配置有独立 socket 控制对照 |
| [#23](https://github.com/skill215/sctp/issues/23) | 地址查询和 PeelOff 拒绝 signed 32-bit 不可表示的关联 ID | id±2^32、int64 极值不能选择其他关联；拒绝后合法 ID 仍能 PeelOff 并传输 payload；架构单测确认非法查询未调用 kernel callback |
| [#24](https://github.com/skill215/sctp/issues/24) | 统一非阻塞 one-to-many 测试 setup，以真实 payload/元数据确认就绪 | 低 GC 发现，SIGURG 和独立 syscall 确认；新增带信号的 setup/PeelOff/payload 回归 |

参数验证保留 unsupported stub 的 ErrUnsupported 优先级，增加专门回归。
公开签名、原始 SCTPConnect errno、标准 deadline 不支持的合同均保持。

## 最终验证

- Go 1.26.10、1.27.3：各五个独立的全库 race/shuffle 进程通过，seed 为
  72611061–72611065。针对性回归每版本再执行 count=3。
- 两版本的 vet、strict checkptr、GOGC=1 全库 race，以及 RawSockaddr、
  Ancillary、Codec、AddressRoundTrip fuzz 全部通过。每个 fuzz 预算 10 秒，
  输入最小化预算 1 秒。上述主记录共 26 个通过的命令组。
- 最终测试源在 ARM、ARM64、MIPS、MIPS64、PPC64、s390x 上，各用两版本
  交叉编译通过，共 12 个编译命令组。本机没有执行 QEMU。
- 核心修复后，两版本的 Linux/386 原生 stub 测试、强制执行的命名空间
  接口名冲突测试 count=3、独立 API 消费者编译、README 示例执行均通过，
  共 8 个命令组。后续改动仅为 Linux 测试夹具，不改变这些检查的库代码。
- Solaris 的 Go 1.26 库构建通过；已知范围外测试编译缺口继续延期，不修改。

本地使用一个 Linux/amd64 内核。这里不代表第二个原生内核、Windows 原生
执行、QEMU 或远端完整发布矩阵已经通过。五轮复查也没有替代发布流程每个
native job 十轮验证的要求。

## 保留失败记录及原因

最初修复后的一次 Go 1.26 低 GC 检查在旧 TestPeelOffCloseOnExec 的阻塞建连
setup 返回 EISCONN；这条失败没有被覆盖为成功。无显式信号的独立 100 次重跑
通过。随后 SIGURG probe 对包装函数和直接 CONNECTX3 各运行 1000 次，都复现
EISCONN，且原生 peer 查询及 Accept 证明关联已建立。

据此登记并修复 #24 的夹具假设，而非改变原始 syscall 接口去吞掉任意 EISCONN。
统一夹具后，针对性 GOGC=1/race 三次测试和最终完整两版本验证通过。
最终复查未再确认其他独立 bug。

## 证据与交付状态

[最终命令](repro/fixes-2026-11-06/checks.json)、
[架构编译](repro/fixes-2026-11-06/architecture.json)、
[兼容检查](repro/fixes-2026-11-06/compatibility.json)、
[结果摘要](repro/fixes-2026-11-06/results.json)、
[修复前后证据说明](repro/fixes-2026-11-06/README.md)、
[源码指纹](repro/fixes-2026-11-06/source-manifest.json)。

#23 和 #24 已用英文创建并回读核对；所有问题保持 open，避免把本地修复描述为
已经合并或发布。代码、回归和文档位于工作区，未提交、push 或发布 release。
架构结论及下一阶段的组织/错误模型建议见
[架构 review](architecture-review-2026-11-06.md)。
