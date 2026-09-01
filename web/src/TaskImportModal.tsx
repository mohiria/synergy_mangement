import { client } from "./api/client";
import type { components } from "./api/schema";
import ImportSkeleton, { type AssembleContext, type ImportField } from "./import/ImportSkeleton";

type ProjectMember = components["schemas"]["ProjectMember"];
type ImportTaskItem = components["schemas"]["ImportTaskItem"];
type KeyResult = components["schemas"]["KeyResult"];

// 任务导入器（AC-02b，裁决 B1）：只导任务，所属 KR 用编号定位。
// 步骤与解析在 import/ImportSkeleton 里（#105），这里只给字段定义与结构组装。
// 入口的权限由页面按 project.canEdit 派生字段控制，规则本身在域层（CanImportTasks）。

type FieldKey = "krCode" | "taskName" | "taskOwner" | "startDate" | "endDate";

const FIELDS: ImportField<FieldKey>[] = [
  { key: "krCode", label: "所属 KR 编号", guess: /KR|关键结果/ },
  { key: "taskName", label: "任务名称", guess: /任务名|任务$/ },
  { key: "taskOwner", label: "任务负责人", guess: /负责人/, person: true },
  { key: "startDate", label: "开始日期", guess: /开始/ },
  { key: "endDate", label: "截止日期", guess: /截止|结束/ },
];

// 裁决 #164：导入模板不再含「预期交付物」列，导入保持轻量，其余字段导入后在抽屉补。
const TEMPLATE_COLUMNS = [
  { header: "所属 KR 编号", sample: "KR1.1" },
  { header: "任务名称", sample: "盘点三套核心库对象与不兼容项" },
  { header: "任务负责人", sample: "陈牧阳" },
  { header: "开始日期", sample: "2026-03-09" },
  { header: "截止日期", sample: "2026-03-26" },
];

const normalizeDate = (s: string) => {
  const m = s.trim().replace(/[./]/g, "-").match(/^(\d{4})-(\d{1,2})-(\d{1,2})$/);
  if (!m) return "";
  return `${m[1]}-${m[2].padStart(2, "0")}-${m[3].padStart(2, "0")}`;
};

type Group = { keyResultId: number; code: string; tasks: ImportTaskItem[] };

// 未填负责人的任务行数：这些行没有可覆盖的姓名，统一指派选中后不再计数（#106 同口径）。
function countTasksWithoutOwner({ rows, cell, fallbackPerson }: AssembleContext<FieldKey>) {
  if (fallbackPerson) return 0;
  return rows.filter((r) => cell(r, "taskName") && !cell(r, "taskOwner")).length;
}

// 结构组装：所属 KR 按编号定位（编号是持久字段，前端只取不算）；KR 列留空沿用上一行。
function makeAssemble(krByCode: Map<string, KeyResult>) {
  return ({ rows, cell, resolvePerson, fallbackPerson }: AssembleContext<FieldKey>) => {
    const out: Group[] = [];
    const byId = new Map<number, Group>();
    const problems: string[] = [];
    let curCode = "";
    for (const r of rows) {
      const v = (key: FieldKey) => cell(r, key);
      if (v("krCode")) curCode = v("krCode").trim();
      if (!v("taskName")) continue;
      const kr = krByCode.get(curCode);
      if (!kr) {
        problems.push(
          curCode
            ? `任务「${v("taskName")}」的所属 KR 编号「${curCode}」在本项目内不存在`
            : `任务「${v("taskName")}」没有所属 KR 编号`,
        );
        continue;
      }
      const ownerName = v("taskOwner");
      const ownerId = ownerName ? resolvePerson(ownerName) : fallbackPerson;
      if (ownerName && !resolvePerson(ownerName)) {
        problems.push(`任务负责人「${ownerName}」未匹配到项目成员`);
      }
      const sd = normalizeDate(v("startDate"));
      const ed = normalizeDate(v("endDate"));
      if (!sd || !ed) {
        problems.push(`任务「${v("taskName")}」缺少可识别的开始/截止日期`);
      }
      let g = byId.get(kr.id);
      if (!g) {
        g = { keyResultId: kr.id, code: kr.code, tasks: [] };
        byId.set(kr.id, g);
        out.push(g);
      }
      g.tasks.push({
        name: v("taskName"),
        ownerId: ownerId ?? 0,
        startDate: sd,
        endDate: ed,
      });
    }
    return { structure: out, problems };
  };
}

export default function TaskImportModal({
  open,
  projectId,
  members,
  krList,
  onClose,
  onImported,
}: {
  open: boolean;
  projectId: number;
  members: ProjectMember[];
  krList: KeyResult[];
  onClose: () => void;
  onImported: () => void;
}) {
  const krByCode = new Map(krList.map((k) => [k.code, k]));
  return (
    <ImportSkeleton<FieldKey, Group[]>
      open={open}
      title="批量导入任务"
      subtitle="字段映射 → 人员匹配 → 结构预览 → 生成任务"
      intro="选择 CSV／xlsx 文件，或从 Excel 复制后直接粘贴（首行为表头）；所属 KR 用编号（如 KR1.1）定位，留空表示沿用上一行。"
      pastePlaceholder={"所属 KR 编号\t任务名称\t任务负责人\t开始日期\t截止日期"}
      templateFileName="任务导入模板.xlsx"
      templateColumns={TEMPLATE_COLUMNS}
      fields={FIELDS}
      members={members}
      assemble={makeAssemble(krByCode)}
      isEmpty={(s) => s.length === 0}
      fallbackPersonSlot={{
        label: "有 {n} 条任务没填负责人",
        hint: "这些行没有可覆盖的姓名，选一个成员统一指派；导入后可在任务详情逐条改",
        missing: countTasksWithoutOwner,
      }}
      renderPreview={(structure) => (
        <>
          {structure.map((g) => (
            <div key={g.keyResultId} style={{ marginBottom: 10 }}>
              <b>
                {g.code} · {g.tasks.length} 项任务
              </b>
              {g.tasks.map((t, i) => (
                <div key={i} className="muted" style={{ marginLeft: 16, marginTop: 4, fontSize: 14 }}>
                  {t.name} · {members.find((m) => m.userId === t.ownerId)?.displayName ?? "？"} ·{" "}
                  {t.startDate || "?"}—{t.endDate || "?"}
                </div>
              ))}
            </div>
          ))}
        </>
      )}
      previewNote={
        <div className="notice">导入后任务直接进入正式任务池（初始状态未开始）；所属 KR 负责人会收到入池通知。</div>
      }
      successMessage={(structure) =>
        `已导入 ${structure.reduce((n, g) => n + g.tasks.length, 0)} 项任务，已直接入池`
      }
      submit={async (structure, sourceFileName) => {
        const res = await client.POST("/projects/{projectId}/import-tasks", {
          params: { path: { projectId } },
          body: {
            items: structure.map((g) => ({ keyResultId: g.keyResultId, tasks: g.tasks })),
            sourceFileName,
          },
        });
        return res.data ? null : (res.error?.message ?? "导入失败");
      }}
      onClose={onClose}
      onDone={onImported}
    />
  );
}
