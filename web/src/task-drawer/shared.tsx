import type { components } from "../api/schema";
import type { TreeTransferItem } from "../TreeTransfer";

// 任务详情抽屉与全部任务页共用的小工具（#109）：抽屉抽成独立模块后，
// 这些两边都用得到的常量与纯函数放这里，避免各留一份。
// 这里不含任何业务规则——规则在服务端，界面只消费派生字段。

type Task = components["schemas"]["Task"];
type TaskStatus = components["schemas"]["TaskStatus"];
type ProjectMember = components["schemas"]["ProjectMember"];
type EdgeType = components["schemas"]["EdgeType"];
type MemberRole = components["schemas"]["MemberRole"];

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

// 候选项文案：新建输入时要列出全部关系类型，此时还没有边可取派生字段，只能在前端枚举。
// 已存在边的显示文案一律取后端的 edgeTypeLabel（F1）。
export const EDGE_TYPE_LABEL: Record<EdgeType, string> = {
  hard_prerequisite: "硬前置交付",
  information: "信息输入",
  handover: "正式成果接收",
  feedback: "迭代／反馈",
};

// 「动态与讨论」Tab 上段默认只展示最近 5 条动态，其余折在「展开全部」后面。
export const ACTIVITY_PREVIEW = 5;

// 成员树分组（AC-53）：原型按团队分组，当前数据模型只有项目成员角色，
// 故按角色分组；团队字段补齐后改为按团队分组即可，组件本身不变。
const MEMBER_GROUP_LABEL: Record<MemberRole, string> = {
  admin: "项目管理员",
  member: "项目成员",
  viewer: "访客",
};
const MEMBER_GROUP_ORDER: MemberRole[] = ["admin", "member", "viewer"];

export function memberTreeItems(members: ProjectMember[]): TreeTransferItem[] {
  return [...members]
    .sort((a, b) => MEMBER_GROUP_ORDER.indexOf(a.role) - MEMBER_GROUP_ORDER.indexOf(b.role))
    .map((m) => ({
      key: String(m.userId),
      groups: [{ key: m.role, label: MEMBER_GROUP_LABEL[m.role] }],
      label: (
        <span className="tree-transfer-row">
          <span className="avatar">{m.displayName.slice(0, 1)}</span>
          <span className="tree-transfer-text">
            <b title={m.displayName}>{m.displayName}</b>
            <small title={m.username}>{m.username}</small>
          </span>
        </span>
      ),
      search: `${m.displayName} ${m.username} ${MEMBER_GROUP_LABEL[m.role]}`.toLowerCase(),
    }));
}

// 结构变更（输入、输入源、输出、接收方）属关键字段：要经所属 KR 负责人审批（AC-23）。
// 回包里带着待审批的结构变更单，就说明这次没有立即生效。
export function pendingStructureChange(task: Task | undefined): boolean {
  const fc = task?.fieldChange;
  return !!fc && fc.state === "pending" && fc.changeType === "structure";
}

export function structureMessage(task: Task | undefined, applied: string): string {
  return pendingStructureChange(task) ? "已提交，待所属 KR 负责人审批后生效" : applied;
}
