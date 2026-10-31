## ADDED Requirements

### Requirement: Every blocking issue has causal regression evidence
14 条阻塞项 SHALL 各有针对修复后契约的正式回归、基线失败证据和正常行为或原始系统调用对照。相似症状 SHALL 按独立根因验证，不能靠放宽断言、跳过或改变触发条件取得通过。

#### Scenario: Regression is promoted from an audit fixture
- **WHEN** 将历史 .txt 探针转为正式测试
- **THEN** 测试 SHALL 验证 green 契约，保留基线失败原因，并把期待未修复行为的观察控制单独处理

#### Scenario: Optional improvement remains unimplemented
- **WHEN** #13 被延期
- **THEN** 发布记录 SHALL 明示其 P3 非阻塞状态，不能声称已修复，也不将其习惯性 nil 断言作为发布门禁

### Requirement: Native and architecture checks state their coverage
发布 SHALL 通过 Go 1.26.x、1.27.x 各十轮独立进程 race/shuffle 测试，以及 vet、严格 checkptr、低 GOGC、fuzz 和 namespace 回归。架构与平台检查 SHALL 区分完整 SCTP、非 SCTP ABI、编译及 unsupported 资源生命周期。

#### Scenario: Native Linux validation
- **WHEN** 执行发布验收
- **THEN** 每个 Go 版本 SHALL 有十轮完整测试；#1 SHALL 在至少两个记录内核版本的原生 Linux 环境有验证证据

#### Scenario: Architecture and unsupported checks
- **WHEN** 六种架构通过 QEMU ABI 测试或下游跨平台编译
- **THEN** 记录 SHALL 不将其等同于完整 SCTP 关联测试；Linux/386 和 Windows 资源清理 SHALL 有实际运行证据

#### Scenario: Namespace capability is required by CI
- **WHEN** 发布 job 执行数字接口名与索引冲突测试
- **THEN** 测试 SHALL 实际运行三轮，不以 skip 成功代替覆盖

### Requirement: Release tags and publication follow validation
新版本 SHALL 使用带注释的规范标签 v0.7.2；发布工作流 SHALL 支持该标签，并依赖所有必需 job。旧标签 SHALL 保留既有指向，独立 Go 模块 SHALL 验证版本解析。

#### Scenario: Candidate is prepared
- **WHEN** 尚未创建正式标签
- **THEN** 系统 SHALL 先验证候选提交、示例和下游编译，不提前声称 go get @v0.7.2 已成功

#### Scenario: Publication completes
- **WHEN** 全部门禁成功且正式标签已创建
- **THEN** release SHALL 指向该标签，并核对 go get @v0.7.2 解析为规范版本，发布说明 SHALL 包含实际验证范围及已知能力限制
