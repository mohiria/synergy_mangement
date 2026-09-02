import { Input, Modal } from "antd";
import type { components } from "../api/schema";

type Task = components["schemas"]["Task"];

// 任务列表行与任务详情抽屉共用的小弹窗（#109）：关闭任务。
// 抽出来只是为了两边不各留一份 JSX；打开状态与提交动作仍由各自的宿主持有。

export function CancelTaskModal({
  task,
  reason,
  onReasonChange,
  onClose,
  onSubmit,
}: {
  task: Task | null;
  reason: string;
  onReasonChange: (v: string) => void;
  onClose: () => void;
  onSubmit: (task: Task, reason: string) => Promise<void> | void;
}) {
  return (
    <Modal
      title="关闭任务"
      open={!!task}
      okText="确认关闭"
      cancelText="返回"
      okButtonProps={{ danger: true, disabled: !reason.trim() }}
      onCancel={onClose}
      onOk={async () => {
        if (task) await onSubmit(task, reason.trim());
        onClose();
      }}
    >
      <p className="muted" style={{ marginTop: 0 }}>
        关闭即时生效并写入任务动态（裁决 10：项目管理员直接操作，无审批环节）；已关闭任务不计入 KR 进度汇总。
      </p>
      <Input.TextArea
        rows={3}
        maxLength={500}
        placeholder="关闭原因（必填）"
        value={reason}
        onChange={(e) => onReasonChange(e.target.value)}
      />
    </Modal>
  );
}
