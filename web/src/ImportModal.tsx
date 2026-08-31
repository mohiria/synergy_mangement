import { client } from "./api/client";
import type { components } from "./api/schema";
import ImportSkeleton, { type AssembleContext, type ImportField } from "./import/ImportSkeleton";

type ProjectMember = components["schemas"]["ProjectMember"];
type ImportRequest = components["schemas"]["ImportRequest"];
type ImportKrItem = components["schemas"]["ImportKrItem"];

// O／KR 导入器（AC-02，裁决 B1）：只导 O 与 KR，不承载任务列——任务走全部任务页的导入器。
// 步骤与解析都在 import/ImportSkeleton 里（#105），这里只给字段定义与结构组装：
// 行归属按表格常规——O 列留空时沿用上一行（fill-down）。

type FieldKey = "oTitle" | "krDescription" | "krOwner" | "krMetric" | "startDate" | "endDate";

// 顺序即表头猜测的优先级：先到先得，所以「KR 负责人」要排在泛化的「负责人」之前。
const FIELDS: ImportField<FieldKey>[] = [
  { key: "oTitle", label: "O 标题", guess: /^O|目标/ },
  { key: "krOwner", label: "KR 负责人", guess: /负责人/, person: true },
  { key: "krDescription", label: "KR 描述", guess: /KR|关键结果/ },
  { key: "krMetric", label: "量化指标", guess: /指标/ },
  { key: "startDate", label: "周期开始", guess: /开始/ },
  { key: "endDate", label: "周期截止", guess: /截止|结束/ },
];

const TEMPLATE_COLUMNS = [
  { header: "O 标题", sample: "核心业务系统数据库国产化替换" },
  { header: "KR 描述", sample: "完成三套核心库的兼容性评估与改造清单" },
  { header: "KR 负责人", sample: "陈牧阳" },
  { header: "量化指标", sample: "不兼容对象 100% 登记并给出改造方案" },
  { header: "周期开始", sample: "2026-03-09" },
  { header: "周期截止", sample: "2026-05-23" },
];

const normalizeDate = (s: string) => {
  const m = s.trim().replace(/[./]/g, "-").match(/^(\d{4})-(\d{1,2})-(\d{1,2})$/);
  if (!m) return "";
  return `${m[1]}-${m[2].padStart(2, "0")}-${m[3].padStart(2, "0")}`;
};

type ODraft = { title: string; keyResults: ImportKrItem[] };

// 未指定负责人的 KR 行数：这些行没有可覆盖的姓名，统一指派选中后就不再计数（#106）。
function countKrWithoutOwner({ rows, cell, fallbackPerson }: AssembleContext<FieldKey>) {
  if (fallbackPerson) return 0;
  return rows.filter((r) => cell(r, "krDescription") && !cell(r, "krOwner")).length;
}

// 结构组装（fill-down）：O 列留空的行沿用上一行的归属。
function assemble({ rows, cell, resolvePerson, fallbackPerson }: AssembleContext<FieldKey>) {
  const out: ODraft[] = [];
  const problems: string[] = [];
  let curO: ODraft | null = null;
  for (const r of rows) {
    const v = (key: FieldKey) => cell(r, key);
    if (v("oTitle")) {
      curO = { title: v("oTitle"), keyResults: [] };
      out.push(curO);
    }
    if (v("krDescription")) {
      if (!curO) {
        problems.push(`KR「${v("krDescription")}」前缺少 O 标题行`);
        continue;
      }
      const ownerName = v("krOwner");
      if (ownerName && !resolvePerson(ownerName)) {
        problems.push(`KR 负责人「${ownerName}」未匹配到项目成员`);
      }
      const sd = normalizeDate(v("startDate"));
      const ed = normalizeDate(v("endDate"));
      if (v("startDate") && !sd) problems.push(`KR「${v("krDescription")}」的周期开始不是可识别的日期`);
      if (v("endDate") && !ed) problems.push(`KR「${v("krDescription")}」的周期截止不是可识别的日期`);
      curO.keyResults.push({
        description: v("krDescription"),
        metric: v("krMetric") || undefined,
        ownerId: ownerName ? resolvePerson(ownerName) : fallbackPerson,
        startDate: sd || undefined,
        endDate: ed || undefined,
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
      title="导入 O / KR 表格"
      subtitle="字段映射 → 人员匹配 → 结构预览 → 生成 O / KR"
      intro="选择 CSV／xlsx 文件，或从 Excel 复制后直接粘贴（首行为表头）；O 列留空表示沿用上一行。任务请到「全部任务」页导入。"
      pastePlaceholder={"O 标题	KR 描述	KR 负责人	量化指标	周期开始	周期截止"}
      templateFileName="OKR导入模板.xlsx"
      templateColumns={TEMPLATE_COLUMNS}
      fields={FIELDS}
      members={members}
      assemble={assemble}
      isEmpty={(s) => s.length === 0}
      fallbackPersonSlot={{
        label: "有 {n} 条 KR 没填负责人",
        hint: "这些行没有可覆盖的姓名，选一个成员统一指派；导入后可在 OKR 页逐条改",
        missing: countKrWithoutOwner,
      }}
      renderPreview={(structure) => (
        <>
          {structure.map((o, oi) => (
            <div key={oi} style={{ marginBottom: 10 }}>
              <b>
                O{oi + 1} {o.title}
              </b>
              {o.keyResults.map((k, ki) => (
                <div key={ki} className="muted" style={{ marginLeft: 16, marginTop: 4, fontSize: 14 }}>
                  KR：{k.description}
                  {k.metric ? ` · ${k.metric}` : ""}
                  {k.ownerId
                    ? ` · 负责人 ${members.find((m) => m.userId === k.ownerId)?.displayName ?? ""}`
                    : ""}
                  {k.startDate || k.endDate ? ` · ${k.startDate ?? "?"}—${k.endDate ?? "?"}` : ""}
                </div>
              ))}
            </div>
          ))}
        </>
      )}
      previewNote={
        <div className="notice">只导入 O 与 KR；任务在「全部任务」页用任务导入器批量导入。</div>
      }
      successMessage={(structure) =>
        `已导入 ${structure.length} 个 O、${structure.reduce((n, o) => n + o.keyResults.length, 0)} 条 KR`
      }
      submit={async (structure, sourceFileName) => {
        const body: ImportRequest = {
          items: structure.map((o) => ({
            title: o.title,
            keyResults: o.keyResults,
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
