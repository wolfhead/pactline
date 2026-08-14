import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ExternalLink, GitMerge, Link2, RefreshCw, Unlink } from "lucide-react";
import { getTaskDelivery, linkTaskCodeChange, unlinkTaskCodeChange, type DeliveryComparison, type CodeChangeObservationStatus, type TaskDelivery, type TaskCodeChange } from "@/api/task-delivery";
import { listTaskStageClaims } from "@/api/task-workflow";
import { ProblemError } from "@/api/v1/client";
import { useIdentity } from "@/identity";
import type { Task, TaskStageClaim } from "@/task-types";

const OBSERVATION_LABELS: Record<CodeChangeObservationStatus, string> = {
  confirmed: "已确认",
  missing: "无法找到",
  unauthorized: "鉴权失败",
  unreachable: "暂时不可用",
  disconnected: "连接已停用",
};

const COMPARISON_LABELS: Record<DeliveryComparison, string> = {
  unchanged: "与提交执行时一致",
  moved: "提交后有新提交",
  merged: "已合并",
  missing: "无法找到",
  unauthorized: "鉴权失败",
  unreachable: "暂时不可用",
  disconnected: "连接已停用",
};

const CODE_CHANGE_STATE_LABELS = {
  opened: "打开",
  closed: "已关闭",
  merged: "已合并",
  locked: "已锁定",
} as const;

function shortSHA(value: string) {
  return value ? value.slice(0, 8) : "未知";
}

function observedAt(value: string) {
  return new Date(value).toLocaleString();
}

function providerLabel(value: TaskCodeChange["provider"]) {
  return value === "github" ? "GitHub" : "GitLab";
}

function repositoryLabel(value: string) {
  try {
    const parsed = new URL(value);
    const repositoryPath = parsed.pathname
      .replace(/\/-\/merge_requests\/\d+.*$/, "")
      .replace(/\/pull\/\d+.*$/, "")
      .replace(/^\//, "")
      .replace(/\/$/, "");
    return `${parsed.host}/${repositoryPath}`;
  } catch {
    return value;
  }
}

function codeChangeState(state: keyof typeof CODE_CHANGE_STATE_LABELS, draft: boolean) {
  return `${CODE_CHANGE_STATE_LABELS[state]}${draft ? " · 草稿" : ""}`;
}

function CodeChangeRow({ codeChange, canUnlink, onUnlink }: { codeChange: TaskCodeChange; canUnlink: boolean; onUnlink: () => void }) {
  const observation = codeChange.latest_observation;
  const symbol = codeChange.kind === "pull_request" ? "#" : "!";
  return (
    <li className="flex items-start gap-3 py-3 first:pt-0 last:pb-0">
      <GitMerge className="mt-0.5 size-4 shrink-0 text-secondary" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <a href={codeChange.web_url} target="_blank" rel="noreferrer" className="inline-flex max-w-full items-center gap-1 text-sm font-medium text-accent hover:underline">
          <span className="truncate">
            {providerLabel(codeChange.provider)} · {repositoryLabel(codeChange.repository_url)} · {symbol}
            {codeChange.change_number} {observation.title || codeChange.web_url}
          </span>
          <ExternalLink className="size-3.5 shrink-0" aria-hidden="true" />
        </a>
        <p className="mt-1 text-xs leading-5 text-fg-muted">
          {codeChangeState(observation.state, observation.draft)} · {observation.source_branch || "未知分支"} → {observation.target_branch || "未知分支"} · {shortSHA(observation.head_sha)} · {OBSERVATION_LABELS[observation.status]}，{observedAt(observation.observed_at)}
        </p>
      </div>
      {canUnlink && (
        <button type="button" onClick={onUnlink} className="inline-flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-xs text-danger hover:bg-danger-subtle">
          <Unlink className="size-3.5" aria-hidden="true" />
          取消关联
        </button>
      )}
    </li>
  );
}

export default function TaskCodeChanges({ task, onChanged }: { task: Task; onChanged: () => Promise<void> }) {
  const { me } = useIdentity();
  const [delivery, setDelivery] = useState<TaskDelivery | null>(null);
  const [claims, setClaims] = useState<TaskStageClaim[]>([]);
  const [codeChangeURL, setCodeChangeURL] = useState("");
  const [taskVersion, setTaskVersion] = useState(task.version);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const activeNumberRef = useRef(task.number);
  activeNumberRef.current = task.number;

  useEffect(() => {
    setTaskVersion(task.version);
  }, [task.version]);

  const reload = useCallback(async () => {
    const forNumber = task.number;
    setLoading(true);
    try {
      const [nextDelivery, nextClaims] = await Promise.all([getTaskDelivery(forNumber), listTaskStageClaims(forNumber)]);
      if (activeNumberRef.current !== forNumber) return;
      setDelivery(nextDelivery);
      setClaims(nextClaims);
      setError("");
    } catch (reason) {
      if (activeNumberRef.current === forNumber) setError((reason as Error).message);
    } finally {
      if (activeNumberRef.current === forNumber) setLoading(false);
    }
  }, [task.number]);

  useEffect(() => {
    void reload();
  }, [reload, me?.id, task.version]);

  const executionClaim = useMemo(() => claims.find((claim) => claim.status === "active" && claim.stage === "execution" && claim.claimed_by.type === "user" && claim.claimed_by.user_id === me?.id) ?? null, [claims, me?.id]);

  async function link(event: FormEvent) {
    event.preventDefault();
    if (!executionClaim || !codeChangeURL.trim() || pending) return;
    const forNumber = task.number;
    setPending(true);
    setError("");
    try {
      const result = await linkTaskCodeChange(forNumber, taskVersion, executionClaim, codeChangeURL.trim());
      if (activeNumberRef.current !== forNumber) return;
      setTaskVersion(result.task.version);
      setCodeChangeURL("");
      await Promise.all([reload(), onChanged()]);
    } catch (reason) {
      if (activeNumberRef.current !== forNumber) return;
      if (reason instanceof ProblemError && reason.code === "VERSION_CONFLICT") {
        await Promise.all([reload(), onChanged()]);
        setError("任务交付信息已更新，已加载最新版本，请重试。");
      } else {
        setError((reason as Error).message);
      }
    } finally {
      if (activeNumberRef.current === forNumber) setPending(false);
    }
  }

  async function unlink(codeChange: TaskCodeChange) {
    if (!executionClaim || pending) return;
    const forNumber = task.number;
    setPending(true);
    setError("");
    try {
      const result = await unlinkTaskCodeChange(forNumber, taskVersion, executionClaim, codeChange.id);
      if (activeNumberRef.current !== forNumber) return;
      setTaskVersion(result.task.version);
      await Promise.all([reload(), onChanged()]);
    } catch (reason) {
      if (activeNumberRef.current !== forNumber) return;
      await Promise.all([reload(), onChanged()]);
      setError((reason as Error).message);
    } finally {
      if (activeNumberRef.current === forNumber) setPending(false);
    }
  }

  return (
    <section aria-labelledby="task-delivery-title" className="border-t border-border pt-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 id="task-delivery-title" className="text-sm font-medium text-fg">
            PR / MR 交付
          </h3>
          <p className="mt-0.5 text-xs leading-5 text-fg-muted">Pull Request / Merge Request 是工作交付证据；是否接受任务仍由本轮验收结果决定。</p>
        </div>
        <button type="button" disabled={loading} onClick={() => void reload()} aria-label="刷新代码交付状态" className="grid size-8 shrink-0 place-items-center rounded-md text-fg-muted hover:bg-surface-subtle hover:text-fg disabled:opacity-50">
          <RefreshCw className={`size-4 ${loading ? "animate-spin" : ""}`} aria-hidden="true" />
        </button>
      </div>

      {executionClaim && (
        <form onSubmit={(event) => void link(event)} className="mt-3 flex flex-col gap-2 sm:flex-row">
          <label htmlFor={`task-${task.number}-code-change-url`} className="sr-only">
            Pull Request 或 Merge Request 地址
          </label>
          <input id={`task-${task.number}-code-change-url`} type="url" required value={codeChangeURL} onChange={(event) => setCodeChangeURL(event.target.value)} placeholder="https://github.com/owner/repository/pull/42" className="h-9 min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-3 text-sm outline-none focus:border-accent focus:ring-3 focus:ring-accent/20" />
          <button type="submit" disabled={pending || !codeChangeURL.trim()} className="inline-flex h-9 items-center justify-center gap-1.5 rounded-md bg-accent px-3 text-xs font-medium text-accent-fg disabled:cursor-wait disabled:opacity-50">
            <Link2 className="size-3.5" aria-hidden="true" />
            {pending ? "正在确认…" : "关联 PR / MR"}
          </button>
        </form>
      )}

      {error && (
        <div role="alert" className="mt-3 flex items-center justify-between gap-3 rounded-md bg-danger-subtle px-3 py-2 text-sm text-danger">
          <span>代码交付加载或更新失败：{error}</span>
          <button type="button" onClick={() => void reload()} className="shrink-0 rounded-md px-2 py-1 text-xs font-medium hover:bg-danger/10">
            重试加载
          </button>
        </div>
      )}

      {delivery?.review && (
        <div className="mt-4 rounded-lg bg-surface-subtle p-3">
          <p className="text-xs font-semibold text-fg">第 {delivery.review.review_cycle} 轮提交执行快照</p>
          {delivery.review.code_changes.length === 0 ? (
            <p className="mt-2 text-xs text-fg-muted">本轮完成执行时没有关联代码变更。</p>
          ) : (
            <ul className="mt-2 divide-y divide-border">
              {delivery.review.code_changes.map(({ snapshot, current, comparison }) => (
                <li key={snapshot.task_code_change_id} className="py-2.5 first:pt-0 last:pb-0">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <a href={snapshot.web_url} target="_blank" rel="noreferrer" className="text-sm font-medium text-accent hover:underline">
                      {providerLabel(snapshot.provider)} · {repositoryLabel(snapshot.web_url)} · {snapshot.kind === "pull_request" ? "#" : "!"}
                      {snapshot.change_number} {snapshot.title || snapshot.web_url}
                    </a>
                    <span className={`rounded-full px-2 py-0.5 text-xs ${comparison === "unchanged" || comparison === "merged" ? "bg-secondary-subtle text-secondary" : "bg-warning/10 text-warning"}`}>{COMPARISON_LABELS[comparison]}</span>
                  </div>
                  <p className="mt-1 text-xs text-fg-muted">
                    冻结 {codeChangeState(snapshot.state, snapshot.draft)} / {shortSHA(snapshot.head_sha)} · 当前 {current ? `${codeChangeState(current.latest_observation.state, current.latest_observation.draft)} / ${shortSHA(current.latest_observation.head_sha)}` : "不可读取"} · 快照于 {observedAt(snapshot.observed_at)}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {loading && !delivery ? (
        <p className="mt-3 text-sm text-fg-muted">正在读取代码交付状态…</p>
      ) : error && !delivery ? null : delivery && delivery.active_links.length > 0 ? (
        <ul className="mt-4 divide-y divide-border">
          {delivery.active_links.map((codeChange) => (
            <CodeChangeRow key={codeChange.id} codeChange={codeChange} canUnlink={Boolean(executionClaim) && !pending} onUnlink={() => void unlink(codeChange)} />
          ))}
        </ul>
      ) : (
        <p className="mt-3 text-sm text-fg-muted">当前没有关联代码变更。</p>
      )}
    </section>
  );
}
