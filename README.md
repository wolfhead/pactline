# Bounty Board

作品制研发任务管理系统。机制设计见 [`docs/mechanism-design.md`](docs/mechanism-design.md),
系统规格见 [`docs/superpowers/specs/2026-07-26-bounty-board-design.md`](docs/superpowers/specs/2026-07-26-bounty-board-design.md)。

## 核心概念

记录单位是**作品**,不是人。一条记录在生命周期前半段是可认领的「单」,
完成后是带 Credits 署名的「作品」。个人页面是「我有署名的作品集合」,
系统不生产排名。

## 本地运行

```bash
make up          # 启动 PostgreSQL(:5433)
make run         # 启动后端(:8080),自动执行迁移与用户 seed
make web-install # 首次
make web-dev     # 启动前端(:5173)
```

打开 http://localhost:5173

## 测试

```bash
make test        # Go,需 Docker(内部执行 `go test ./... -p 1`,共用一个数据库,不能并发跑多个 make test)
make web-test     # 前端单元测试
```

## Phase 1 边界

当前为 Phase 1,实现核心闭环与 Credits 署名。以下**尚未实现**,按规格分阶段推进:

| Phase | 内容 |
|---|---|
| 2 | 计分与定档、结算快照、锚点清单 |
| 3 | 承诺日期、逾期公开预警、改约(含阻塞指名)、月度瓶颈报告 |
| 4 | 业务线聚合(投入 / 产出 / 比值) |
| 5 | Owner 契约与基线账、`BASELINE` credit 自动生成 |
| 6 | 飞书 OAuth 登录与通知 |

## 认证

Phase 1 **没有认证**。身份由 `X-User-Id` 请求头携带,前端提供用户切换器。
迁移中 seed 了六个内置用户覆盖各角色组合。

接入飞书 OAuth 时,`internal/api/identity.go` 的 `withIdentity` 与
`web/src/identity.tsx` 的 `UserSwitcher` 一并移除。

## 两条不可回归的规则

1. **未确认的 credit 不进入任何统计与展示** —— 防的是虚假署名
2. **`ABANDONED` 的单出现在作品流中,并带着结论** —— 失败带结论归档,是让人敢碰难题的机制

两条均有回归测试守护,见 `internal/store/credit_store_test.go`、
`internal/api/feed_handler_test.go` 与端到端闭环测试 `internal/api/loop_test.go`。

## 状态流转与权限要点

开单(`POST /api/bounties`)要求调用者持有 `SPONSOR` 或 `STEWARD` 角色;
仅有 `ENGINEER` 角色的身份会被拒绝(403)。

`DELIVERED -> COMPLETED`(验收)只能由该单的 sponsor 或 steward 触发,
认领人不能自己验收自己交付的单 —— 认领人仍可交单(`CLAIMED -> DELIVERED`)、
放弃(`-> ABANDONED`,须填写 retrospective)以及把交付单交还认领状态
(`DELIVERED -> CLAIMED`)。
