import { Input, Modal } from "antd";
import type { components } from "../api/schema";

type Task = components["schemas"]["Task"];

// 任务列表行与任务详情抽屉共用的三个小弹窗（#109）：入池退回、更新进度、申请取消。
// 抽出来只是为了两边不各留一份 JSX；打开状态与提交动作仍由各自的宿主持有。

export function PoolRejectModal({
  task,
  opinion,
  onOpinionChange,
  onClose,
  onSubmit,
}: {
  task: Task | null;
  opinion: string;
  onOpinionChange: (v: string) => void;
  onClose: () => void;
  onSubmit: (task: Task, opinion: string) => Promise<void> | void;
}) {
  return (
    <Modal
      title="退回入池申请"
      open={!!task}
      okText="确认退回"
      cancelText="取消"
      onCancel={onClose}
      okButtonProps={{ danger: true, disabled: !opinion.trim() }}
      onOk={async () => {
        if (task) await onSubmit(task, opinion.trim());
        onClose();
      }}
    >
      <p className="muted" style={{ marginTop: 0 }}>
        退回后任务回到草稿，提交人可在「待我处理」看到退回理由并修改后重新提交。退回意见必填。
      </p>
      <Input.TextArea
        rows={3}
        maxLength={500}
        placeholder="退回意见（必填）"
        value={opinion}
        onChange={(e) => onOpinionChange(e.target.value)}
      />
    </Modal>
  );
}

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
      title="申请关闭任务"
      open={!!task}
      okText="提交关闭申请"
      cancelText="返回"
      okButtonProps={{ danger: true, disabled: !reason.trim() }}
      onCancel={onClose}
      onOk={async () => {
        if (task) await onSubmit(task, reason.trim());
        onClose();
      }}
    >
      <p className="muted" style={{ marginTop: 0 }}>
        关闭须经所属 KR 负责人审批（KR 负责人在本人负责 KR 下免审即时生效）；已关闭任务不计入 KR 进度汇总。
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
