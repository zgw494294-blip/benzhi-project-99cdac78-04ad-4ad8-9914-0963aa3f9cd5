# oral-archive-release

`oral-archive-release` 是面向语言档案馆的口述录音研究开放审查服务。它把录音编目、参与者身份引用、知情同意材料、访问限制、整改证据、负责人决定和最终研究访问凭据汇集到一个可追溯案件中。

服务提供版本化 JSON HTTP API `/api/v1`，主要用户是语言档案编目员、知情同意审查员和档案开放负责人。所有写请求都要求 `actorId`、对应的 `actorRole`、`idempotencyKey`，除创建案件外还要求 `expectedVersion`。陈旧版本会返回稳定错误码 `VERSION_CONFLICT`；同一幂等键和相同载荷会重放原始响应快照，不同载荷会返回 `IDEMPOTENCY_CONFLICT`。所有错误响应均包含 `requestId`。

## 状态流程

案件按下列状态推进：

`DRAFT` → `IN_REVIEW` → `CHANGES_REQUIRED` → `APPROVED` → `FROZEN` → `RELEASED`

- 责任编目员可在 `DRAFT` 阶段修订案件资料或说明理由后移交责任，并可原子批量登记参与者、录音和同意材料。提交前系统计算每位参与者对每条录音的有效用途、访问人群、有效期、撤回状态和敏感主题授权。
- 合规报告可按指定评估时点和预警窗口计算尚未生效、已过期、已撤回、有效及即将到期的授权。审查员可把全部或指定异常原子导入为去重的结构化问题。
- 编目员可把补充同意材料、脱敏修订及其覆盖的问题组成证据包；审查员选择覆盖该问题的证据包逐项接受或拒绝。
- 开放负责人可先读取与实际批准规则一致的就绪检查和 `readinessDigest`；批准时携带该摘要可阻止依据已变化的状态作出决定。
- 批准后系统冻结录音修订、同意材料摘要、适用范围和批准决定的精确清单。清单摘要采用确定性 JSON 编码和 SHA-256 计算。
- 冻结清单支持按录音追溯精确修订、相关授权、访问范围和批准依据。冻结后签发递增编号的不可变凭据；区段核验会逐节点定位序号、前序摘要、凭据摘要、清单摘要或索引不一致。

## 构建、运行与测试

构建全部包：

```text
go build ./...
```

默认仅监听高位回环地址 `127.0.0.1:19081`：

```text
go run ./cmd/archive-release -data-dir=.benzhi/archive-release-data
```

也可显式指定回环监听地址：

```text
go run ./cmd/archive-release -addr=127.0.0.1:19181 -data-dir=.benzhi/archive-release-data
```

或设置端口号形式的 `PORT`。当同时提供 `PORT` 与 `-addr` 时，两者必须解析为同一个 `127.0.0.1:<PORT>`。服务拒绝 `0.0.0.0` 和非回环地址。

运行测试：

```text
go test ./...
```

运行有界自检：

```text
go run ./cmd/archive-release -selfcheck -addr=127.0.0.1:19081 -data-dir=.benzhi/selfcheck-data
```

自检会真实启动 HTTP 监听，通过公开 API 完成创建案件、编目录音和同意材料、提交审查、登记问题、脱敏整改、复核、批准、冻结、签发、凭据核验和审计时间线查询，然后有界关闭服务并返回明确退出码。

## API 概览

- `POST /api/v1/release-cases`：创建开放案件。
- `GET /api/v1/release-cases/{caseId}`：读取案件快照。
- `PATCH /api/v1/release-cases/{caseId}`：修订草稿资料或移交责任。
- `POST /api/v1/release-cases/{caseId}/catalog-batches`：原子批量登记参与者、录音和同意材料。
- `POST /api/v1/release-cases/{caseId}/participants`：登记参与者身份引用。
- `POST /api/v1/release-cases/{caseId}/recordings`：登记录音及初始修订。
- `POST /api/v1/release-cases/{caseId}/consents`：登记或补充同意材料。
- `GET /api/v1/release-cases/{caseId}/compliance?evaluateAt=...&warningDays=...`：按时点评估授权范围和到期预警。
- `POST /api/v1/release-cases/{caseId}/submit-review`：提交审查。
- `POST /api/v1/release-cases/{caseId}/findings`：登记结构化问题。
- `POST /api/v1/release-cases/{caseId}/findings/compliance-import`：把合规异常原子导入为去重问题。
- `POST /api/v1/release-cases/{caseId}/evidence-packages`：登记补充材料、脱敏修订及问题关联证据包。
- `POST /api/v1/release-cases/{caseId}/revisions`：登记脱敏录音修订。
- `POST /api/v1/release-cases/{caseId}/findings/{findingId}/review`：复核整改证据。
- `POST /api/v1/release-cases/{caseId}/decision`：批准或退回案件。
- `GET /api/v1/release-cases/{caseId}/approval-readiness`：读取批准阻断明细和就绪摘要。
- `POST /api/v1/release-cases/{caseId}/freeze`：冻结精确开放清单。
- `GET /api/v1/release-cases/{caseId}/manifest`：读取只读冻结清单。
- `GET /api/v1/release-cases/{caseId}/manifest/recordings/{recordingId}`：校验整份清单后读取录音条目溯源。
- `POST /api/v1/release-cases/{caseId}/credentials`：签发不可变访问凭据。
- `GET /api/v1/credentials/{credentialNo}`：按编号读取凭据。
- `GET /api/v1/credentials/{credentialNo}/verify`：核验清单、凭据和链式摘要。
- `GET /api/v1/credentials/{credentialNo}/chain?length=3`：核验到目标凭据的受限连续链区段并定位首个失败节点。
- `GET /api/v1/release-cases/{caseId}/timeline`：读取案件审计时间线。
- `GET /api/v1/release-cases/{caseId}/overview`：读取案件、合规评估和时间线汇总。
- `GET /healthz`：健康检查。

## 本地持久化

`-data-dir` 下保存带 `schemaVersion` 和 SHA-256 校验摘要的案件快照、事务记录、幂等响应快照、只追加审计链及凭据索引。单进程提交锁串行化写入，文件通过同目录临时文件、`fsync` 和原子替换提交。启动时会校验领域不变量和摘要，利用事务记录恢复未完成的快照或索引；损坏事务会被隔离并返回可诊断错误，不会静默丢弃。

当前实现不依赖外部数据库或第三方服务。
