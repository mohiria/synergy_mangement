import { client } from "./api/client";
import type { components } from "./api/schema";
import ImportSkeleton, { type AssembleContext, type ImportField } from "./import/ImportSkeleton";

type ProjectMember = components["schemas"]["ProjectMember"];
type ImportRequest = components["schemas"]["ImportRequest"];
type ImportKrItem = components["schemas"]["ImportKrItem"];
type ImportTaskItem = components["schemas"]["ImportTaskItem"];

// 表格导入（AC-02）：选择表格 → 字段映射 → 人员匹配 → 结构预览 → 生成 O/KR 与任务草稿。
// 步骤与解析都在 import/ImportSkeleton 里（#105），这里只给字段定义与结构组装：
// 行归属按表格常规——O/KR 列留空时沿用上一行（fill-down）。

type FieldKey =
  | "oTitle"
  | "krDescription"
  | "krOwner"
  | "krMetric"
  | "taskName"
  | "taskOwner"
  | "startDate"
  | "endDate"
  | "deliverable";

// 顺序即表头猜测的优先级：先到先得，所以「KR 负责人」要排在泛化的「负责人」之前。
const FIELDS: ImportField<FieldKey>[] = [
  { key: "oTitle", label: "O 标题", guess: /^O|目标/ },
  { key: "krOwner", label: "KR 负责人", guess: /KR.*负责人|结果负责人/, person: true },
  { key: "krDescription", label: "KR 描述", guess: /KR|关键结果/ },
  { key: "krMetric", label: "量化指标", guess: /指标/ },
  { key: "taskName", label: "任务名称", guess: /任务名|任务$/ },
  { key: "taskOwner", label: "任务负责人", guess: /负责人/, person: true },
  { key: "startDate", label: "开始日期", guess: /开始/ },
  { key: "endDate", label: "截止日期", guess: /截止|结束/ },
  { key: "deliverable", label: "预期交付物", guess: /交付/ },
];

const TEMPLATE_COLUMNS = [
  { header: "O 标题", sample: "核心业务系统数据库国产化替换" },
  { header: "KR 描述", sample: "完成三套核心库的兼容性评估与改造清单" },
  { header: "KR 负责人", sample: "陈牧阳" },
  { header: "量化指标", sample: "不兼容对象 100% 登记并给出改造方案" },
  { header: "任务名称", sample: "盘点三套核心库对象与不兼容项" },
  { header: "任务负责人", sample: "陈牧阳" },
  { header: "开始", sample: "2026-03-09" },
  { header: "截止", sample: "2026-03-26" },
  { header: "预期交付物", sample: "核心库不兼容对象清单" },
];

const normalizeDate = (s: string) => {
  const m = s.trim().replace(/[./]/g, "-").match(/^(\d{4})-(\d{1,2})-(\d{1,2})$/);
  if (!m) return "";
  return `${m[1]}-${m[2].padStart(2, "0")}-${m[3].padStart(2, "0")}`;
};

type KrDraft = ImportKrItem & { tasks: ImportTaskItem[] };
type ODraft = { title: string; keyResults: KrDraft[] };

// 结构组装（fill-down）：O、KR 列留空的行沿用上一行的归属。
function assemble({ rows, cell, resolvePerson }: AssembleContext<FieldKey>) {
  const out: ODraft[] = [];
  const problems: string[] = [];
  let curO: ODraft | null = null;
  let curKr: KrDraft | null = null;
  for (const r of rows) {
    const v = (key: FieldKey) => cell(r, key);
    if (v("oTitle")) {
      curO = { title: v("oTitle"), keyResults: [] };
      out.push(curO);
      curKr = null;
    }
    if (v("krDescription")) {
      if (!curO) {
        problems.push(`KR「${v("krDescription")}」前缺少 O 标题行`);
        continue;
      }
      curKr = {
        description: v("krDescription"),
        metric: v("krMetric") || undefined,
        ownerId: v("krOwner") ? resolvePerson(v("krOwner")) : undefined,
        tasks: [],
      };
      if (v("krOwner") && !resolvePerson(v("krOwner"))) {
        problems.push(`KR 负责人「${v("krOwner")}」未匹配到项目成员`);
      }
      curO.keyResults.push(curKr);
    }
    if (v("taskName")) {
      if (!curKr) {
        problems.push(`任务「${v("taskName")}」前缺少 KR 行`);
        continue;
      }
      const ownerName = v("taskOwner");
      const ownerId = ownerName ? resolvePerson(ownerName) : undefined;
      if (!ownerId) {
        problems.push(`任务负责人「${ownerName || "（空）"}」未匹配到项目成员`);
      }
      const sd = normalizeDate(v("startDate"));
      const ed = normalizeDate(v("endDate"));
      if (!sd || !ed) {
        problems.push(`任务「${v("taskName")}」缺少可识别的开始/截止日期`);
      }
      curKr.tasks.push({
        name: v("taskName"),
        ownerId: ownerId ?? 0,
        startDate: sd,
        endDate: ed,
        expectedDeliverable: v("deliverable") || undefined,
      });
    }
  }
  return { structure: out, problems };
}

export default function ImportModal({
  open,
  projectId,
  members,
  onClose,
  onImported,
}: {
  open: boolean;
  projectId: number;
  members: ProjectMember[];
  onClose: () => void;
  onImported: () => void;
}) {
  return (
    <ImportSkeleton<FieldKey, ODraft[]>
      open={open}
      title="导入已有表格"
      subtitle="字段映射 → 人员匹配 → 结构预览 → 生成 O/KR 与任务草稿"
      intro="选择 CSV／xlsx 文件，或从 Excel 复制后直接粘贴（首行为表头）；O、KR 列留空表示沿用上一行。"
      pastePlaceholder={"O 标题\tKR 描述\tKR 负责人\t任务名称\t任务负责人\t开始\t截止\t预期交付物"}
      templateFileName="OKR与任务导入模板.xlsx"
      templateColumns={TEMPLATE_COLUMNS}
      fields={FIELDS}
      members={members}
      assemble={assemble}
      isEmpty={(s) => s.length === 0}
      renderPreview={(structure) => (
        <>
          {structure.map((o, oi) => (
            <div key={oi} style={{ marginBottom: 10 }}>
              <b>
                O{oi + 1} {o.title}
              </b>
              {o.keyResults.map((k, ki) => (
                <div key={ki} style={{ marginLeft: 16, marginTop: 4 }}>
                  <span>
                    KR：{k.description}
                    {k.ownerId && (
                      <span className="muted">
                        　负责人 {members.find((m) => m.userId === k.ownerId)?.displayName}
                      </span>
                    )}
                  </span>
                  {k.tasks.map((tk, ti) => (
                    <div key={ti} className="muted" style={{ marginLeft: 16, fontSize: 14 }}>
                      任务：{tk.name} ·{" "}
                      {members.find((m) => m.userId === tk.ownerId)?.displayName ?? "？"} ·{" "}
                      {tk.startDate || "?"}—{tk.endDate || "?"}
                      {tk.expectedDeliverable ? ` · ${tk.expectedDeliverable}` : ""}
                    </div>
                  ))}
                </div>
              ))}
            </div>
          ))}
        </>
      )}
      previewNote={
        <div className="notice">导入后任务为草稿；请到「全部任务」按 KR 勾选并批量提交入池审批。</div>
      }
      successMessage={(structure) =>
        `已导入 ${structure.length} 个 O、${structure.reduce(
          (n, o) => n + o.keyResults.reduce((k, kr) => k + kr.tasks.length, 0),
          0,
        )} 项任务草稿；请在全部任务页按 KR 批量提交入池`
      }
      submit={async (structure, sourceFileName) => {
        const body: ImportRequest = {
          items: structure.map((o) => ({
            title: o.title,
            keyResults: o.keyResults.map((k) => ({
              description: k.description,
              metric: k.metric,
              ownerId: k.ownerId,
              tasks: k.tasks,
            })),
          })),
        };
        const res = await client.POST("/projects/{projectId}/import", {
          params: { path: { projectId } },
          body: { ...body, sourceFileName },
        });
        return res.data ? null : (res.error?.message ?? "导入失败");
      }}
      onClose={onClose}
      onDone={onImported}
    />
  );
}
