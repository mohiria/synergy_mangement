import type { components } from "../api/schema";

// 任务详情抽屉与全部任务页共用的小工具（#109）：抽屉抽成独立模块后，
// 这些两边都用得到的常量与纯函数放这里，避免各留一份。
// 这里不含任何业务规则——规则在服务端，界面只消费派生字段。

type Task = components["schemas"]["Task"];
type TaskStatus = components["schemas"]["TaskStatus"];

/** KR 下拉与成员／任务树共用的轻量 KR 视图。 */
export type KrOption = {
  id: number;
  objectiveId: number;
  code: string;
  description: string;
  ownerId?: number;
};

// 状态色（与我的工作、项目总览各页一致）；状态文案一律消费 API 的 statusLabel（AC-04）。
export const STATUS_CLASS: Record<TaskStatus, string> = {
  not_started: "",
  waiting_input: "warning",
  in_progress: "in_progress",
  pending_intermediate_review: "review",
  pending_final_review: "review",
  completed: "completed",
  cancelled: "",
};

export const fmtDate = (d?: string | null) => (d ? d.slice(5).replace("-", ".") : "—");
export const fmtTime = (s?: string) => (s ? s.slice(0, 16).replace("T", " ") : "");

// #173 裁决：关系类型删除，只留必要性；已存在边的显示文案一律取后端的 necessityLabel。

// 「动态与讨论」Tab 上段默认只展示最近 5 条动态，其余折在「展开全部」后面。
export const ACTIVITY_PREVIEW = 5;

// 结构变更（输入、输入源、接收方）属关键字段：#172 裁决后直接生效，
// 提示文案不再区分「待审批」分支。
export function structureMessage(_task: Task | undefined, applied: string): string {
  return applied;
}
