import { message } from "antd";
import { client } from "../api/client";
import type { components } from "../api/schema";

type Task = components["schemas"]["Task"];

// 任务列表行与任务详情抽屉都能触发的几个单任务动作（#109）。
// 抽成模块级函数是为了让抽屉不依赖宿主页面的局部状态，同时两边不各留一份实现。
// 这里只发请求与提示，成功后由调用方决定刷新什么；规则一律在服务端。

/** 每个动作成功后回调，让调用方刷新自己的数据。 */
type OnDone = () => void;

export async function startTask(projectId: number, task: Task, done: OnDone) {
  const res = await client.POST("/projects/{projectId}/tasks/{taskId}/update-status", {
    params: { path: { projectId, taskId: task.id } },
    body: { status: "in_progress" },
  });
  if (res.data) {
    message.success("已开始执行");
    done();
  } else {
    message.error(res.error?.message ?? "操作失败");
  }
}

export async function submitPoolReview(projectId: number, task: Task, done: OnDone) {
  const res = await client.POST("/projects/{projectId}/tasks/{taskId}/submit-pool-review", {
    params: { path: { projectId, taskId: task.id } },
  });
  if (res.data) {
    message.success("已提交所属 KR 负责人入池审批");
    done();
  } else {
    message.error(res.error?.message ?? "提交失败");
  }
}

export async function decidePoolReview(
  projectId: number,
  task: Task,
  decision: "approved" | "rejected",
  opinion: string | undefined,
  done: OnDone,
) {
  const res = await client.POST("/projects/{projectId}/tasks/{taskId}/pool-review-decision", {
    params: { path: { projectId, taskId: task.id } },
    body: { decision, opinion },
  });
  if (res.data) {
    message.success(decision === "approved" ? "已通过，任务进入未开始" : "已退回，任务回到草稿");
    done();
  } else {
    message.error(res.error?.message ?? "处理失败");
  }
}

export async function cancelTask(projectId: number, task: Task, reason: string, done: OnDone) {
  const res = await client.POST("/projects/{projectId}/tasks/{taskId}/cancellation", {
    params: { path: { projectId, taskId: task.id } },
    body: { reason },
  });
  if (res.data) {
    // AC-57：除 KR 负责人本人负责 KR 下免审外，取消要经所属 KR 负责人审批。
    message.success(
      res.data.status === "cancelled" ? "任务已取消" : "取消申请已提交，待所属 KR 负责人审批",
    );
    done();
  } else {
    message.error(res.error?.message ?? "操作失败");
  }
}

export async function saveProgress(
  projectId: number,
  task: Task,
  progress: number | null,
  done: OnDone,
) {
  const res = await client.PUT("/projects/{projectId}/tasks/{taskId}/progress", {
    params: { path: { projectId, taskId: task.id } },
    body: progress === null ? {} : { progress },
  });
  if (res.data) {
    message.success(progress === null ? "已清除进度" : "进度已更新");
    done();
  } else {
    message.error(res.error?.message ?? "操作失败");
  }
}
