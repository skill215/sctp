# v0.8.0 发布方案

制定日期：2026-11-09。状态：用户已授权按此方案执行，发布结果以 GitHub #31 为准。
GitHub 跟踪：https://github.com/skill215/sctp/issues/31。
用户已于 2026-11-09 授权：验证通过后合并、打 tag 并发布；跟踪 Issue 保留最终 SHA、工作流、模块校验和及消费证据。

## 版本与基线

- 已核对远端最新正式 Release 为 [v0.7.4](https://github.com/skill215/sctp/releases/tag/v0.7.4)，尚无 v0.8 标签。
- 建议目标 **v0.8.0**：相对 v0.7.4 新增完整 U2–U5 和 A–E 能力，包含标准 deadline 行为变化及 SocketConfig.Messages 字段。原 v0.7.2 规划也把 Context/deadline/消息/通知列为后续功能版本方向；本次不因此补齐旧规划中的所有其他愿望项。
- 候选：PR #30，head `bb739b612088d46bc16025c361ee49f9c58ed1a2`，base master `62ad388be958f0bd19d29d63dfc05743726779a1`。PR 当前 open/draft。
- 已可下载的候选伪版本：`v0.7.5-0.20261109072213-bb739b612088`。伪版本来自已有标签与提交位置，不要求正式发布为 v0.7.5；本轮也不额外发布 v0.7.5。

## 发布范围与兼容性

范围冻结为 PR #30：opt-in 消息 I/O、五类 typed notifications、连接标准 deadline/Context 解析与拨号，以及 A–E 的 AcceptContext、有界建立门面、socket timeout/绑定快照、资源查询、消息连续性/协商流边界。

必须在 Release Notes 明示：

1. 相对正式 v0.7.4，SocketConfig 新增 Messages 字段，旧 unkeyed literal 需要改用 keyed literal；“A–E 未增加旧结构字段”只适用于 U2–U5 候选基线，不能作为整个 release 的兼容声明。
2. nil Messages 保留 legacy 路径；非 nil 模式禁止冲突 callback/旧 I/O 或选项修改，缺失或不一致 metadata、流越界、restart 等按契约拒绝或要求关闭。
3. 标准 deadline 在支持平台上开始生效；SetSocketTimeouts 是独立的 SO timeout 能力，部分失败须关闭接管中的连接。
4. Go >=1.26、生产 CGO_ENABLED=0、支持 Linux 内核 SCTP（不含 Linux/386）；其他平台的 stub/ABI 检查不扩展支持承诺。
5. 账目版本 `bounded-setup-v2/linux/<GOARCH>`。N16/Z256/D4096、Go1.26.10/Linux amd64 示例：Dialer=8960、Listener=48524、Establish=78380、Connection=9516；candidate=87340，listener+旧连接+candidate=145380。以运行平台查询为准；不保证 RSS，总量还须加调用方缓冲和业务预算。

不纳入：v1 重写、新平台、动态 stream reset、自动重连/重放、Diameter 协议及 diameterkit 生产代码迁移。

## 当前证据

| 项目 | 结果 |
| --- | --- |
| Review | 修复 1 个 P2：分片后 peek 成功而 consume 零进展终止错误遗漏关闭门；无剩余阻塞项 |
| 本地完整原生 / race | 各 352 个通过事件，0 fail/skip |
| 生命周期及连续性 race ×20 | 660 个通过事件，0 fail/skip |
| 远端候选 CI | [run 42579273839](https://github.com/skill215/sctp/actions/runs/42579273839)，14/14 jobs success |
| 原生矩阵 | 两 Ubuntu × 两 Go；每任务 10 轮 race、checkptr、低 GC、5×100万 fuzz、必跑 namespace、README/消费者 |
| 架构与支持边界 | 6 QEMU + Linux/386/Windows 两 Go；ARM/MIPS 的 64位 int 专用测试合计 6 次明确 skip |
| 远端精确模块消费 | GOWORK=off、无 replace、全新缓存；原生和 race SCTP 示例通过，70 个源码哈希吻合，go mod verify 通过 |

候选 go.sum（不能复制为正式版本的校验和）：
```
github.com/skill215/sctp v0.7.5-0.20261109072213-bb739b612088 h1:3TLlV+PUP7wbICn6l80EtGodrDiK31rIqHV3q28Ri8w=
github.com/skill215/sctp v0.7.5-0.20261109072213-bb739b612088/go.mod h1:g/XAScdNEAqQa2cjZdtxXVkxIJZEy77f7/fmCkTD2gg=
```

证据：[候选消费记录](https://github.com/skill215/sctp/pull/30#issuecomment-6734645807)、[最终 CI 记录](https://github.com/skill215/sctp/pull/30#issuecomment-6734763054)。

## 执行顺序与门禁

- [x] 确认现有正式版本、候选、范围、兼容风险和工作流。
- [x] 准备 Release Notes 草稿并将方案记录到 GitHub。
- [x] 已获得执行发布指令并复核 PR/head/base 与远端 v0.8.0 tag/release 状态；如基线有变更，审查差异并更新候选证据。
- [x] 在 PR #30 分支增加正式 `releases/v0.8.0.md`，内容采用已审阅草稿；同步交付文档的候选/发布状态与链接，记录本方案。保护原工作区未提交及 v1 文件。
- [ ] 对新增 notes 的最终 PR head 完成 CI；即便仅文档变化，也使用该 head 的检查记录。源码变化须补相应回归，不能借用旧 SHA 的结果。
- [ ] 将 PR 置为 ready，确认无阻塞审查且最新检查通过后合并 master，记录实际合并 SHA。
- [ ] master 的 notes 变更触发既有 release 工作流，在该合并 SHA 再执行完整验证矩阵。全部成功后 publish job 创建指向该 SHA 的 annotated v0.8.0 tag，创建 GitHub Release，并执行精确版本模块获取检查。
- [ ] 等待整条 release 工作流（含 notes job）成功，核对 tag 对象为 annotated、解引用 commit 与已验证合并 SHA 相同、Release 正文完整；不能只依据可修改的 target_commitish 字段。
- [ ] 独立新模块、GOWORK=off、无 replace、全新缓存下载 `github.com/skill215/sctp@v0.8.0`，记录实际版本、来源 SHA、module/go.mod 两条 sum；执行消费者原生/race、go mod verify，核对发布源码与候选 Go 文件。核验 @latest 的真实结果。
- [ ] 回填 GitHub 记录：PR/合并 SHA/tag/Release URL、发布 workflow、skip、正式 go.sum、消费结果，再关闭发布跟踪 Issue。
- [ ] diameterkit 另一个 session 锁定正式版本、迁移薄适配器并完成第64/65批验收。该项是下游交接，不能计为本上游已通过。

### 工作流注意点

已检查 `.github/workflows/release.yaml`：master 上 `releases/*.md` 变更即触发验证后发布，本发布候选已加入正式 notes，合并即进入发布流程。执行使用上述 notes→merge→validate→tag/release 路径，无须先手工打 tag；也不在旧候选 SHA 提前打正式标签。

现有工作流已经检查不可覆盖旧 tag、annotated tag 类型及目标 SHA；无需为此次方案改写发布机制。notes job 会同步已有 Release 正文，因此发布 PR 不应顺带编辑历史 releases 文件。

### 失败处理

- PR 或合并后的 validate 失败：停止发布，保留失败记录，修复根因后验证新 SHA；不能重复重跑至绿并隐藏失败。
- 未创建 tag 时：可修正尚未发布的 notes/代码，依照现有 pending-note 恢复流程重新验证。
- tag 已创建或 Release 部分完成时：先确认远端 tag、Release、workflow 的实际状态；不移动/删除已发布 tag，不盲目重跑非幂等的 release-create。仅恢复缺失的记录/Release/验证步骤；若库代码需要修复，另发新补丁版本。
- 依赖方遇到回归时可回退此前锁定版本；后续正式修复使用新版本，不复用 v0.8.0。

## 完成定义

正式发布完成要求上述上游发布步骤全部有证据；最终状态见 GitHub #31。diameterkit 迁移为单独下游任务，不纳入上游发布完成条件。
