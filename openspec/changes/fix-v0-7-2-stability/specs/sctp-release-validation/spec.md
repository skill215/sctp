## ADDED Requirements

### Requirement: Every blocking issue has causal regression evidence
Each of the 14 blockers SHALL have a permanent post-fix contract regression, baseline failure evidence, and normal-behavior or raw-syscall controls. Similar symptoms SHALL be verified by independent root cause rather than made to pass through weaker assertions, skips, or changed triggers.

#### Scenario: Regression is promoted from an audit fixture
- **WHEN** a historical .txt probe becomes a permanent test
- **THEN** it SHALL check the intended passing contract, retain the baseline failure cause, and separate observational controls that expect unfixed behavior

#### Scenario: Optional improvement remains unimplemented
- **WHEN** #13 is deferred
- **THEN** release records SHALL explicitly retain its P3 non-blocking status, without claiming it fixed or making its conventional nil assertion a release gate

### Requirement: Native and architecture checks state their coverage
Release validation SHALL pass ten independent-process race/shuffle rounds on each of Go 1.26.x and 1.27.x, plus vet, strict checkptr, low GOGC, fuzzing, and namespace regressions. Architecture/platform checks SHALL distinguish full SCTP, non-SCTP ABI, compilation, and unsupported-resource lifecycles.

#### Scenario: Native Linux validation
- **WHEN** release acceptance runs
- **THEN** each Go version SHALL complete ten full-suite rounds, and #1 SHALL have evidence on at least two native Linux environments with recorded kernel versions

#### Scenario: Architecture and unsupported checks
- **WHEN** six architectures pass QEMU ABI tests or downstream cross-platform compilation
- **THEN** records SHALL not equate them with full SCTP association tests; Linux/386 and Windows resource cleanup SHALL have actual execution evidence

#### Scenario: Namespace capability is required by CI
- **WHEN** a release job tests numeric interface-name/index conflicts
- **THEN** the test SHALL actually run three times rather than substitute successful skips for coverage

### Requirement: Release tags and publication follow validation
The new release SHALL use canonical annotated tag v0.7.2. The release workflow SHALL support it and depend on every required job. Old tags SHALL retain their targets, and an independent Go module SHALL verify version resolution.

#### Scenario: Candidate is prepared
- **WHEN** the formal tag has not yet been created
- **THEN** the candidate commit, examples, and downstream compilation SHALL be verified first, without prematurely claiming successful go get @v0.7.2

#### Scenario: Publication completes
- **WHEN** all gates pass and the formal tag exists
- **THEN** the release SHALL reference that tag, go get @v0.7.2 SHALL be checked for canonical version resolution, and release notes SHALL state actual validation scope and known capability limits
