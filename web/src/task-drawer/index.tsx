import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Input, Modal, message } from "antd";
import { client } from "../api/client";
import type { components } from "../api/schema";
import FileUploadField from "../FileUploadField";
import TaskDrawer from "./TaskDrawer";
import ConfigureInputModal from "./ConfigureInputModal";
import { CancelTaskModal } from "./modals";
import { cancelTask as apiCancelTask, saveProgress as apiSaveProgress, startTask as apiStartTask } from "./actions";
import { structureMessage, type KrOption } from "./shared";

type Objective = components["schemas"]["Objective"];
type Task = components["schemas"]["Task"];
type ProjectMember = components["schemas"]["ProjectMember"];

// 任务详情抽屉的独立宿主（裁决 E1 第一步、#109）：抽屉与它的全部配套弹窗、动作都在这里，
// 自己取数、自己刷新，任何页面只要给出 projectId 与要打开的 taskId 就能挂载同一个抽屉。
// 宿主页面不需要把自己的局部状态暴露进来；抽屉内动作落库后通过 onChanged 通知宿主刷新列表。

export default function TaskDrawerHost({
  projectId,
  taskId,
  initialTab,
  source,
  onClose,
  onChanged,
  onOpenInGraph,
}: {
  projectId: number;
  /** 要打开的任务；null 表示关闭 */
  taskId: number | null;
  /** 打开时落位的 Tab（站内通知直达用） */
  initialTab?: string;
  /** 来源分组（我的工作卡片带入），决定抽屉内的落位区块 */
  source?: string;
  onClose: () => void;
  /** 抽屉内任何动作落库后回调，宿主刷新自己的列表 */
  onChanged: () => void;
  /** #121：宿主自定义「在关系图谱中查看」行为（协作关系页内改为关抽屉并聚焦）；缺省跳图谱页 */
  onOpenInGraph?: (taskId: number) => void;
}) {
  const navigate = useNavigate();
  const [objectives, setObjectives] = useState<Objective[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [members, setMembers] = useState<ProjectMember[]>([]);

  // 抽屉自己取数：编号、KR 列表与成员都从 API 取，不借宿主页面的状态。
  const load = useCallback(async () => {
    const [objectivesRes, tasksRes, membersRes] = await Promise.all([
      client.GET("/projects/{projectId}/objectives", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/tasks", { params: { path: { projectId } } }),
      client.GET("/projects/{projectId}/members", { params: { path: { projectId } } }),
    ]);
    setObjectives(objectivesRes.data ?? []);
    setTasks(tasksRes.data ?? []);
    setMembers(membersRes.data ?? []);
  }, [projectId]);

  useEffect(() => {
    if (taskId != null) load();
  }, [taskId, load]);

  // 抽屉内的动作落库后：先刷新自己的数据，再让宿主刷新它的列表。
  const refresh = useCallback(() => {
    load();
    onChanged();
  }, [load, onChanged]);

  // O／KR／任务编号都是持久字段（AC-64），前端只取不算。
  const krList: KrOption[] = objectives.flatMap((o) => o.keyResults.map((k) => ({ ...k })));
  const taskCode = new Map(tasks.map((t) => [t.id, t.code]));
  const okrCode = new Map<number, string>();
  objectives.forEach((o) => o.keyResults.forEach((k) => okrCode.set(k.id, `${o.code} / ${k.code}`)));

  // 抽屉内的导航栈（#101）：点开来源任务时压栈，关闭时逐级返回并回到当时的 Tab。
  const [current, setCurrent] = useState<number | null>(taskId);
  const [tab, setTab] = useState(initialTab ?? "overview");
  const [stack, setStack] = useState<{ taskId: number; tab: string }[]>([]);
  useEffect(() => {
    // 宿主换任务（列表点行、通知直达）是一次新的进入：清掉返回栈。
    setCurrent(taskId);
    setTab(initialTab ?? "overview");
    setStack([]);
  }, [taskId, initialTab]);

  const [fcReject, setFcReject] = useState<{ task: Task; changeId: number } | null>(null);
  const [fcRejectOpinion, setFcRejectOpinion] = useState("");
  const [inputTask, setInputTask] = useState<Task | null>(null);
  const [provideReq, setProvideReq] = useState<number | null>(null);
  const [provideText, setProvideText] = useState("");
  const [provideFile, setProvideFile] = useState<File | null>(null);
  const [providing, setProviding] = useState(false);
  const [completionTask, setCompletionTask] = useState<Task | null>(null);
  const [completionNote, setCompletionNote] = useState("");
  const [crReject, setCrReject] = useState<{ task: Task; reviewId: number } | null>(null);
  const [crRejectOpinion, setCrRejectOpinion] = useState("");
  const [cancelTask, setCancelTask] = useState<Task | null>(null);
  const [cancelReason, setCancelReason] = useState("");

  // 列表行与抽屉共用的几个单任务动作放在 actions.ts，两边都只是包一层刷新。
  const startTask = (task: Task) => apiStartTask(projectId, task, refresh);
  const doCancelTask = (task: Task, reason: string) => apiCancelTask(projectId, task, reason, refresh);
  const saveProgress = (task: Task, progress: number | null) =>
    apiSaveProgress(projectId, task, progress, refresh);

  // #172 裁决：变更类审批只剩关闭申请。
  const decideCancelRequest = async (
    task: Task,
    requestId: number,
    decision: "approved" | "rejected",
    opinion?: string,
  ) => {
    const res = await client.POST(
      "/projects/{projectId}/tasks/{taskId}/cancel-requests/{requestId}/decision",
      {
        params: { path: { projectId, taskId: task.id, requestId } },
        body: { decision, opinion },
      },
    );
    if (res.data) {
      message.success(decision === "approved" ? "已通过，任务进入已关闭" : "已退回，任务继续执行");
      refresh();
    } else {
      message.error(res.error?.message ?? "处理失败");
    }
  };

  const abandonCancelRequest = async (task: Task, requestId: number) => {
    const res = await client.POST(
      "/projects/{projectId}/tasks/{taskId}/cancel-requests/{requestId}/abandon",
      { params: { path: { projectId, taskId: task.id, requestId } } },
    );
    if (res.data) {
      message.success("已放弃本次关闭申请");
      refresh();
    } else {
      message.error(res.error?.message ?? "操作失败");
    }
  };

  const remindBlocker = async (targetKey: string) => {
    const res = await client.POST("/projects/{projectId}/reminders", {
      params: { path: { projectId } },
      body: { targetKey },
    });
    if (res.response.ok) {
      message.success("已提醒当前待行动人");
    } else {
      message.error(res.error?.message ?? "提醒失败");
    }
  };

  const acceptInput = async (requestId: number) => {
    const res = await client.POST("/projects/{projectId}/input-requests/{requestId}/accept", {
      params: { path: { projectId, requestId } },
    });
    if (res.data) {
      message.success("已同意接收；提交内容后输入才更新为就绪");
      refresh();
    } else {
      message.error(res.error?.message ?? "操作失败");
    }
  };

  const closeProvide = () => {
    setProvideReq(null);
    setProvideText("");
    setProvideFile(null);
  };

  const provideInput = async () => {
    if (provideReq == null) return;
    setProviding(true);
    const res = await client.POST("/projects/{projectId}/input-requests/{requestId}/provide", {
      params: { path: { projectId, requestId: provideReq } },
      body: {
        text: provideText.trim() || undefined,
        fileName: provideFile?.name,
      },
    });
    if (res.data) {
      if (res.data.uploadUrl && provideFile) {
        try {
          const put = await fetch(res.data.uploadUrl, { method: "PUT", body: provideFile });
          if (!put.ok) throw new Error(String(put.status));
          // 附件先落 uploading，确认写入后输入才转为已提供，下游就绪度随之更新。
          const commit = await client.POST(
            "/projects/{projectId}/input-requests/{requestId}/commit",
            { params: { path: { projectId, requestId: provideReq } } },
          );
          if (!commit.data) throw new Error(commit.error?.message ?? "确认失败");
        } catch {
          message.error("文件上传失败，请确认文件服务可用后重试");
          setProviding(false);
          refresh();
          return;
        }
      }
      message.success("已提交，目标任务输入更新为就绪");
      closeProvide();
      refresh();
    } else {
      message.error(res.error?.message ?? "提交失败");
    }
    setProviding(false);
  };

  const openInputFile = async (requestId: number) => {
    const res = await client.GET("/projects/{projectId}/input-requests/{requestId}/file-url", {
      params: { path: { projectId, requestId } },
    });
    if (res.data) window.location.assign(res.data.url);
  };

  const removeEdge = async (edgeId: number) => {
    const res = await client.DELETE("/projects/{projectId}/edges/{edgeId}", {
      params: { path: { projectId, edgeId } },
    });
    if (res.data) {
      message.success(structureMessage(res.data, "已解除该输入关系"));
      refresh();
    } else {
      message.error(res.error?.message ?? "解除失败");
    }
  };

  const submitCompletion = async (task: Task, note: string) => {
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/completion-reviews", {
      params: { path: { projectId, taskId: task.id } },
      body: { note },
    });
    if (res.data) {
      message.success("完成申请已提交，进入待 KR 终审");
      refresh();
    } else {
      message.error(res.error?.message ?? "提交失败");
    }
  };

  // AC-66：已完成任务发起成果更新——重新开放候选上传，走同一道完成审批，任务状态保持已完成。
  const startResultUpdate = async (task: Task) => {
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/result-update", {
      params: { path: { projectId, taskId: task.id } },
    });
    if (res.data) {
      message.success("已发起成果更新，请重传交付物后提交完成申请");
      refresh();
    } else {
      message.error(res.error?.message ?? "发起失败");
    }
  };

  // MW-09：接收方确认接收，待接收项退出「待我接收」并形成接收记录。
  const confirmReceipt = async (task: Task) => {
    const res = await client.POST("/projects/{projectId}/tasks/{taskId}/confirm-receipt", {
      params: { path: { projectId, taskId: task.id } },
    });
    if (res.data) {
      message.success("已确认接收");
      refresh();
    } else {
      message.error(res.error?.message ?? "确认失败");
    }
  };

  const decideCompletion = async (
    task: Task,
    reviewId: number,
    decision: "approved" | "rejected",
    opinion?: string,
    intermediate?: boolean,
  ) => {
    const res = await client.POST(
      "/projects/{projectId}/tasks/{taskId}/completion-reviews/{reviewId}/decision",
      {
        params: { path: { projectId, taskId: task.id, reviewId } },
        body: { decision, opinion },
      },
    );
    if (res.data) {
      message.success(
        decision === "rejected"
          ? "已退回，候选文件删除，任务回到进行中"
          : intermediate
            ? "或签通过，进入待 KR 终审"
            : "终审通过，候选交付物已覆盖当前内容，任务完成",
      );
      refresh();
    } else {
      message.error(res.error?.message ?? "处理失败");
    }
  };

  return (
    <>
      <TaskDrawer
        projectId={projectId}
        task={tasks.find((t) => t.id === current) ?? null}
        code={current != null ? taskCode.get(current) : undefined}
        taskCode={taskCode}
        okrCode={okrCode}
        members={members}
        activeTab={tab}
        onTabChange={setTab}
        canGoBack={stack.length > 0}
        source={source ?? ""}
        onClose={() => {
          const prev = stack[stack.length - 1];
          if (prev) {
            setStack((st) => st.slice(0, -1));
            setCurrent(prev.taskId);
            setTab(prev.tab);
            return;
          }
          setCurrent(null);
          setStack([]);
          onClose();
        }}
        actions={{
          start: startTask,
          openCancel: (t) => {
            setCancelTask(t);
            setCancelReason("");
          },
          saveProgress,
          // #138 就地编辑；#172 裁决：直接修改立即生效，无修改原因。
          editTaskFields: async (t, changes) => {
            const res = await client.POST("/projects/{projectId}/tasks/{taskId}/edits", {
              params: { path: { projectId, taskId: t.id } },
              body: changes,
            });
            if (res.data) {
              message.success("修改已生效");
              refresh();
              return true;
            }
            message.error(res.error?.message ?? "保存失败");
            return false;
          },
          approveCancelRequest: (t, id) => decideCancelRequest(t, id, "approved"),
          openFcReject: (t, id) => {
            setFcReject({ task: t, changeId: id });
            setFcRejectOpinion("");
          },
          abandonCancelRequest,
          openSubmitCompletion: (t) => {
            setCompletionTask(t);
            setCompletionNote("");
          },
          approveCompletion: (t, id, intermediate) =>
            decideCompletion(t, id, "approved", undefined, intermediate),
          openCrReject: (t, id) => {
            setCrReject({ task: t, reviewId: id });
            setCrRejectOpinion("");
          },
          openConfigureInput: (t) => setInputTask(t),
          // #135：参与人／接收方／成果审核人改就地多选，宿主直接落库后刷新。
          saveReviewers: async (t, userIds) => {
            const res = await client.PUT("/projects/{projectId}/tasks/{taskId}/reviewers", {
              params: { path: { projectId, taskId: t.id } },
              body: { userIds },
            });
            if (res.data) {
              message.success("成果审核人已调整");
              refresh();
            } else {
              message.error(res.error?.message ?? "保存失败");
            }
          },
          saveReceivers: async (t, scope, userIds) => {
            const res = await client.PUT("/projects/{projectId}/tasks/{taskId}/receivers", {
              params: { path: { projectId, taskId: t.id } },
              body: { scope, userIds },
            });
            if (res.data) {
              message.success(structureMessage(res.data, "接收方配置已保存"));
              refresh();
            } else {
              message.error(res.error?.message ?? "保存失败");
            }
          },
          saveParticipants: async (t, userIds) => {
            const res = await client.PUT("/projects/{projectId}/tasks/{taskId}/participants", {
              params: { path: { projectId, taskId: t.id } },
              body: { userIds },
            });
            if (res.data) {
              message.success("参与人已更新");
              refresh();
            } else {
              message.error(res.error?.message ?? "保存失败");
            }
          },
          confirmReceipt,
          startResultUpdate,
          acceptInput,
          openProvide: (id) => {
            setProvideReq(id);
            setProvideText("");
            setProvideFile(null);
          },
          openInputFile,
          remindBlocker,
          removeEdge,
          openTask: (id) => {
            if (id === current) return;
            if (current != null) {
              setStack((st) => [...st, { taskId: current, tab: tab }]);
            }
            setCurrent(id);
            setTab("overview");
          },
          openInGraph: (id) =>
            onOpenInGraph ? onOpenInGraph(id) : navigate(`/projects/${projectId}/graph?task=${id}`),
        }}
      />
      <ConfigureInputModal
        projectId={projectId}
        task={inputTask}
        tasks={tasks}
        taskCode={taskCode}
        objectives={objectives}
        krList={krList}
        members={members}
        onClose={() => setInputTask(null)}
        onSaved={() => {
          setInputTask(null);
          refresh();
        }}
      />
      <Modal
        title="提交输入内容"
        open={provideReq != null}
        okText="提交"
        cancelText="取消"
        confirmLoading={providing}
        okButtonProps={{ disabled: !provideText.trim() && !provideFile }}
        onCancel={closeProvide}
        onOk={provideInput}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          提交后输入请求变为已提供，目标任务输入更新为就绪；文字内容与文件至少提交其一。
        </p>
        <Input.TextArea
          rows={3}
          maxLength={2000}
          placeholder="文字内容"
          value={provideText}
          onChange={(e) => setProvideText(e.target.value)}
        />
        <div style={{ marginTop: 8 }}>
          <FileUploadField value={provideFile} onChange={setProvideFile} />
        </div>
        <div className="notice" style={{ marginTop: 8 }}>
          文件在点击「提交」后才上传；关闭窗口不保留本次选择。
        </div>
      </Modal>
      <Modal
        title="提交完成申请"
        open={!!completionTask}
        okText="提交完成申请"
        cancelText="取消"
        okButtonProps={{ disabled: !completionNote.trim() }}
        onCancel={() => setCompletionTask(null)}
        onOk={async () => {
          if (completionTask) {
            await submitCompletion(completionTask, completionNote.trim());
          }
          setCompletionTask(null);
        }}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          本次全部候选交付物整体提交；已配置成果审核人时进入多人或签，否则直接进入待 KR 终审。
        </p>
        <Input.TextArea
          rows={3}
          maxLength={500}
          placeholder="提交说明（必填）"
          value={completionNote}
          onChange={(e) => setCompletionNote(e.target.value)}
        />
      </Modal>
      <Modal
        title="退回完成申请"
        open={!!crReject}
        okText="确认退回"
        cancelText="取消"
        okButtonProps={{ danger: true, disabled: !crRejectOpinion.trim() }}
        onCancel={() => setCrReject(null)}
        onOk={async () => {
          if (crReject) {
            await decideCompletion(crReject.task, crReject.reviewId, "rejected", crRejectOpinion.trim());
          }
          setCrReject(null);
        }}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          退回后本次候选文件删除、原当前交付物保持不变，任务回到进行中；退回意见必填。
        </p>
        <Input.TextArea
          rows={3}
          maxLength={500}
          placeholder="退回意见（必填）"
          value={crRejectOpinion}
          onChange={(e) => setCrRejectOpinion(e.target.value)}
        />
      </Modal>
      <Modal
        title="退回关闭申请"
        open={!!fcReject}
        okText="确认退回"
        cancelText="取消"
        onCancel={() => setFcReject(null)}
        okButtonProps={{ danger: true, disabled: !fcRejectOpinion.trim() }}
        onOk={async () => {
          if (fcReject) {
            await decideCancelRequest(fcReject.task, fcReject.changeId, "rejected", fcRejectOpinion.trim());
          }
          setFcReject(null);
        }}
      >
        <p className="muted" style={{ marginTop: 0 }}>
          退回后任务继续执行；提交人会看到退回待处理事项。退回意见必填。
        </p>
        <Input.TextArea
          rows={3}
          maxLength={500}
          placeholder="退回意见（必填）"
          value={fcRejectOpinion}
          onChange={(e) => setFcRejectOpinion(e.target.value)}
        />
      </Modal>
      <CancelTaskModal
        task={cancelTask}
        reason={cancelReason}
        onReasonChange={setCancelReason}
        onClose={() => setCancelTask(null)}
        onSubmit={doCancelTask}
      />
    </>
  );
}
