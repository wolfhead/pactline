# Bounty Board

## Current Task Platform

The primary product is now a task-management platform. Its current delivery
surface includes:

- responsive task list with on-demand task detail;
- a desktop three-column workspace with a half-width task detail pane;
- first-class projects and ordered milestones;
- structured, revisioned acceptance criteria shared by projects, milestones,
  and tasks, with evidence-backed checks and task completion gating;
- task-to-project and task-to-milestone association; and
- audited project and milestone lifecycle transitions;
- invite-only Lark authentication for one company; and
- single-Administrator user management with read-only impersonation.

Project and milestone semantics are defined in
[`docs/superpowers/specs/2026-07-27-projects-and-milestones-design.md`](docs/superpowers/specs/2026-07-27-projects-and-milestones-design.md).
The identity model and operating procedure are documented in
[`docs/operations/lark-identity.md`](docs/operations/lark-identity.md).

作品制研发任务管理系统。机制设计见 [`docs/mechanism-design.md`](docs/mechanism-design.md),
系统规格见 [`docs/superpowers/specs/2026-07-26-bounty-board-design.md`](docs/superpowers/specs/2026-07-26-bounty-board-design.md)。

## 当前状态:机制已移入 legacy

项目方向已调整:新的任务管理平台优先,原先的 bounty/credits 机制作为可选层
后续再接回。**本仓库当前的机制实现(bounty 状态机、credits、作品流、个人页、
计分、结算、定档、锚点清单、steward 修正通道)已整体移入 `internal/legacy`
(后端)与 `web/src/legacy`(前端),挂在 `/api/legacy/...` 路由前缀下,并已从
导航中移除,但仍可通过直接 URL 访问,测试也全部保留、持续跑绿。**
详见 [`internal/legacy/README.md`](internal/legacy/README.md)。以下文档
其余部分描述的仍是这套机制本身的行为——搬家不改变行为,只是换了位置。

下方核心概念、认证、规则等描述的都是 `internal/legacy` / `web/src/legacy`
里的这套机制,不要把它误认成新任务平台的产品行为。

## 核心概念

记录单位是**作品**,不是人。一条记录在生命周期前半段是可认领的「单」,
完成后是带 Credits 署名的「作品」。个人页面是「我有署名的作品集合」,
系统不生产排名。

## 本地运行

```bash
make up          # 启动 PostgreSQL(:5433)
make run         # 启动后端(:8080),自动执行迁移与用户 seed
make web-install # 首次
make web-dev     # 启动前端(:5173),使用 Development 登录
```

打开 http://localhost:5173

## 测试

```bash
make test        # Go,需 Docker(内部执行 `go test ./... -p 1`,共用一个数据库,不能并发跑多个 make test)
make web-test     # 前端组件测试(vitest)
make e2e          # 端到端测试(Playwright,真实浏览器驱动整个应用)
```

`make e2e` 不需要手动起服务:`web/playwright.config.ts` 的 `webServer` 会自动拉起
PostgreSQL(`docker compose up -d --wait`)、Go 后端与 Vite 前端;若开发者本机已经
在跑这三者,则复用现有进程(`reuseExistingServer`),不会被占用端口卡住。

每个 e2e 用例自建自己的数据(标题带唯一后缀)并在测试结束时清理
(`credits` 随其 `bounty` 级联删除),不依赖、也不修改数据库里已有的行 ——
整套 e2e 跑完后 `bounties`、`credits` 的行数应与跑之前完全一致。

## Legacy mechanism boundary

当前为 Phase 1,实现核心闭环与 Credits 署名。以下**尚未实现**,按规格分阶段推进:

| Phase | 内容 |
|---|---|
| 2 | 计分与定档、结算快照、锚点清单 |
| 3 | 承诺日期、逾期公开预警、改约(含阻塞指名)、月度瓶颈报告 |
| 4 | 业务线聚合(投入 / 产出 / 比值) |
| 5 | Owner 契约与基线账、`BASELINE` credit 自动生成 |
| 6 | 已由当前 Lark identity 实现取代 |

## Authentication

The application uses server-owned `bb_session` cookies and a separate
`bb_csrf` cookie/header pair for mutations. Production supports only Lark
OAuth. Development authentication is available only when
`APP_ENV=development` or `test`; production startup rejects it.

There is no production `X-User-Id` fallback and no user switcher. The first
Lark account whose verified tenant email matches `BOOTSTRAP_ADMIN_EMAIL`
becomes the single Administrator. Every later account must enter through a
one-time invitation created by that Administrator.

## 两条不可回归的规则

1. **未确认的 credit 不进入任何统计与展示** —— 防的是虚假署名
2. **`ABANDONED` 的单出现在作品流中,并带着结论** —— 失败带结论归档,是让人敢碰难题的机制

两条均有回归测试守护,见 `internal/legacy/store/credit_store_test.go`、
`internal/legacy/api/feed_handler_test.go` 与端到端闭环测试 `internal/legacy/api/loop_test.go`。

## 状态流转与权限要点

开单(`POST /api/legacy/bounties`)要求调用者持有 `SPONSOR` 或 `STEWARD` 角色;
仅有 `ENGINEER` 角色的身份会被拒绝(403)。

`DELIVERED -> COMPLETED`(验收)只能由该单的 sponsor 或 steward 触发,
认领人不能自己验收自己交付的单 —— 认领人仍可交单(`CLAIMED -> DELIVERED`)、
放弃(`-> ABANDONED`,须填写 retrospective)以及把交付单交还认领状态
(`DELIVERED -> CLAIMED`)。
