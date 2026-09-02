import { client } from "./api/client";
import type { components } from "./api/schema";
import ImportSkeleton, { type AssembleContext, type ImportField } from "./import/ImportSkeleton";

type ProjectMember = components["schemas"]["ProjectMember"];
type ImportRequest = components["schemas"]["ImportRequest"];
type ImportKrItem = components["schemas"]["ImportKrItem"];

// O／KR 导入器（AC-02，裁决 B1）：只导 O 与 KR，不承载任务列——任务走全部任务页的导入器。
// 步骤与解析都在 import/ImportSkeleton 里（#105），这里只给字段定义与结构组装：
// 行归属按表格常规——O 列留空时沿用上一行（fill-down）。
// 裁决 12（#183）：KR 无负责人与周期属性，列缩为 O 标题／KR 描述／量化指标三列。

type FieldKey = "oTitle" | "krDescription" | "krMetric";

const FIELDS: ImportField<FieldKey>[] = [
  { key: "oTitle", label: "O 标题", guess: /^O|目标/ },
  // 旧模板可能还带「KR 负责人」列（裁决 12 前），描述列按「描述／关键结果」认，避免误吞负责人列。
  { key: "krDescription", label: "KR 描述", guess: /描述|关键结果/ },
  { key: "krMetric", label: "量化指标", guess: /指标/ },
];

const TEMPLATE_COLUMNS = [
  { header: "O 标题", sample: "核心业务系统数据库国产化替换" },
  { header: "KR 描述", sample: "完成三套核心库的兼容性评估与改造清单" },
  { header: "量化指标", sample: "不兼容对象 100% 登记并给出改造方案" },
];

type ODraft = { title: string; keyResults: ImportKrItem[] };

// 结构组装（fill-down）：O 列留空的行沿用上一行的归属。
function assemble({ rows, cell }: AssembleContext<FieldKey>) {
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
      curO.keyResults.push({
        description: v("krDescription"),
        metric: v("krMetric") || undefined,
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
      subtitle="字段映射 → 结构预览 → 生成 O / KR"
      intro="选择 CSV／xlsx 文件，或从 Excel 复制后直接粘贴（首行为表头）；O 列留空表示沿用上一行。任务请到「全部任务」页导入。"
      pastePlaceholder={"O 标题	KR 描述	量化指标"}
      templateFileName="OKR导入模板.xlsx"
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
                <div key={ki} className="muted" style={{ marginLeft: 16, marginTop: 4, fontSize: 14 }}>
                  KR：{k.description}
                  {k.metric ? ` · ${k.metric}` : ""}
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
