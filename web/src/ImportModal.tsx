import { useMemo, useState } from "react";
import { Alert, Button, Input, Modal, Select, Steps, message } from "antd";
import { client } from "./api/client";
import type { components } from "./api/schema";

type ProjectMember = components["schemas"]["ProjectMember"];
type ImportRequest = components["schemas"]["ImportRequest"];
type ImportKrItem = components["schemas"]["ImportKrItem"];
type ImportTaskItem = components["schemas"]["ImportTaskItem"];

// 表格导入（AC-02）：粘贴表格 → 字段映射 → 人员匹配 → 结构预览 → 生成 O/KR 与任务草稿。
// 行归属按表格常规：O/KR 列留空时沿用上一行（fill-down）。

type FieldKey =
  | "ignore"
  | "oTitle"
  | "krDescription"
  | "krOwner"
  | "krMetric"
  | "taskName"
  | "taskOwner"
  | "startDate"
  | "endDate"
  | "deliverable";

const FIELD_LABEL: Record<FieldKey, string> = {
  ignore: "忽略此列",
  oTitle: "O 标题",
  krDescription: "KR 描述",
  krOwner: "KR 负责人",
  krMetric: "量化指标",
  taskName: "任务名称",
  taskOwner: "任务负责人",
  startDate: "开始日期",
  endDate: "截止日期",
  deliverable: "预期交付物",
};

// CSV 读取（#97）：Excel 另存的 CSV 有三种常见来源——「CSV UTF-8」带 BOM、
// 中文环境默认的「CSV (逗号分隔)」是 GB18030／GBK、以及纯 UTF-8。
// 先剥 BOM，再用严格 UTF-8 试解；解不出（遇到非法字节序列）才回落 GB18030。
export function decodeTable(buffer: ArrayBuffer): string {
  let bytes = new Uint8Array(buffer);
  if (bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf) {
    bytes = bytes.subarray(3);
  }
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    return new TextDecoder("gb18030").decode(bytes);
  }
}

// 分隔符只看首行（表头）：正文单元格里偶然出现的制表符不该改变整篇的切分口径。
// 计数相同时按 制表符 → 逗号 → 分号 取，剪贴板粘贴的表格因此仍走制表符。
const DELIMITERS = ["\t", ",", ";"] as const;

export function detectDelimiter(text: string): string {
  const header = firstLogicalLine(text);
  let best = ",";
  let bestCount = 0;
  for (const d of DELIMITERS) {
    const n = countOutsideQuotes(header, d);
    if (n > bestCount) {
      best = d;
      bestCount = n;
    }
  }
  return best;
}

// 首行要按引号规则取：被引号包裹的字段里可以有换行，那不是行尾。
function firstLogicalLine(text: string): string {
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c === '"') {
      if (quoted && text[i + 1] === '"') i++;
      else quoted = !quoted;
    } else if (!quoted && (c === "\n" || c === "\r")) {
      return text.slice(0, i);
    }
  }
  return text;
}

function countOutsideQuotes(line: string, delim: string): number {
  let quoted = false;
  let n = 0;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (c === '"') {
      if (quoted && line[i + 1] === '"') i++;
      else quoted = !quoted;
    } else if (!quoted && c === delim) {
      n++;
    }
  }
  return n;
}

// RFC 4180 口径的切分：双引号包裹的字段里可以有分隔符与换行，两个连续引号表示一个引号。
// 全空行（含 ",,,," 这类只有分隔符的行）在这一步就剔除，不进后面的映射与预览。
export function parseDelimited(text: string, delim = detectDelimiter(text)): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  const endField = () => {
    row.push(field.trim());
    field = "";
  };
  const endRow = () => {
    endField();
    if (row.some((c) => c !== "")) rows.push(row);
    row = [];
  };
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          quoted = false;
        }
      } else {
        field += c;
      }
      continue;
    }
    switch (c) {
      case '"':
        quoted = true;
        break;
      case delim:
        endField();
        break;
      case "\r":
        if (text[i + 1] === "\n") i++;
        endRow();
        break;
      case "\n":
        endRow();
        break;
      default:
        field += c;
    }
  }
  if (field !== "" || row.length > 0) endRow();
  return rows;
}

const normalizeDate = (s: string) => {
  const m = s.trim().replace(/[./]/g, "-").match(/^(\d{4})-(\d{1,2})-(\d{1,2})$/);
  if (!m) return "";
  return `${m[1]}-${m[2].padStart(2, "0")}-${m[3].padStart(2, "0")}`;
};

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
  const [step, setStep] = useState(0);
  const [raw, setRaw] = useState("");
  const [mapping, setMapping] = useState<FieldKey[]>([]);
  const [nameOverrides, setNameOverrides] = useState<Record<string, number>>({});
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  // 源文件名随导入记录留存（§7.9、AC-68）：选文件时自动带上，粘贴时留空。
  const [sourceFileName, setSourceFileName] = useState("");

  const rows = useMemo(() => parseDelimited(raw), [raw]);
  const columnCount = rows.reduce((n, r) => Math.max(n, r.length), 0);

  const reset = () => {
    setStep(0);
    setRaw("");
    setSourceFileName("");
    setMapping([]);
    setNameOverrides({});
    setError(null);
  };

  const goMapping = () => {
    if (rows.length === 0) {
      setError("请先粘贴表格内容（支持从 Excel 直接复制）");
      return;
    }
    setError(null);
    // 按表头猜测映射。
    const header = rows[0];
    const guess = (h: string): FieldKey => {
      if (/^O|目标/.test(h)) return "oTitle";
      if (/KR|关键结果/.test(h)) return "krDescription";
      if (/KR.*负责人|结果负责人/.test(h)) return "krOwner";
      if (/指标/.test(h)) return "krMetric";
      if (/任务名|任务$/.test(h)) return "taskName";
      if (/负责人/.test(h)) return "taskOwner";
      if (/开始/.test(h)) return "startDate";
      if (/截止|结束/.test(h)) return "endDate";
      if (/交付/.test(h)) return "deliverable";
      return "ignore";
    };
    setMapping(Array.from({ length: columnCount }, (_, i) => guess(header[i] ?? "")));
    setStep(1);
  };

  // 人员匹配：姓名 → 成员（精确匹配 displayName，否则待人工指定）。
  const memberByName = useMemo(() => {
    const m = new Map<string, number>();
    members.forEach((mm) => m.set(mm.displayName, mm.userId));
    return m;
  }, [members]);

  const dataRows = rows.slice(1);
  const col = (key: FieldKey) => mapping.indexOf(key);

  const personNames = useMemo(() => {
    const names = new Set<string>();
    for (const r of dataRows) {
      for (const key of ["krOwner", "taskOwner"] as FieldKey[]) {
        const c = col(key);
        if (c >= 0 && r[c]) names.add(r[c]);
      }
    }
    return [...names];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, mapping]);

  const resolvePerson = (name: string): number | undefined =>
    nameOverrides[name] ?? memberByName.get(name);

  // 结构组装（fill-down）。
  const structure = useMemo(() => {
    type KrDraft = ImportKrItem & { tasks: ImportTaskItem[] };
    type ODraft = { title: string; keyResults: KrDraft[] };
    const out: ODraft[] = [];
    const problems: string[] = [];
    let curO: ODraft | null = null;
    let curKr: KrDraft | null = null;
    for (const r of dataRows) {
      const cell = (key: FieldKey) => {
        const c = col(key);
        return c >= 0 ? (r[c] ?? "") : "";
      };
      if (cell("oTitle")) {
        curO = { title: cell("oTitle"), keyResults: [] };
        out.push(curO);
        curKr = null;
      }
      if (cell("krDescription")) {
        if (!curO) {
          problems.push(`KR「${cell("krDescription")}」前缺少 O 标题行`);
          continue;
        }
        curKr = {
          description: cell("krDescription"),
          metric: cell("krMetric") || undefined,
          ownerId: cell("krOwner") ? resolvePerson(cell("krOwner")) : undefined,
          tasks: [],
        };
        if (cell("krOwner") && !resolvePerson(cell("krOwner"))) {
          problems.push(`KR 负责人「${cell("krOwner")}」未匹配到项目成员`);
        }
        curO.keyResults.push(curKr);
      }
      if (cell("taskName")) {
        if (!curKr) {
          problems.push(`任务「${cell("taskName")}」前缺少 KR 行`);
          continue;
        }
        const ownerName = cell("taskOwner");
        const ownerId = ownerName ? resolvePerson(ownerName) : undefined;
        if (!ownerId) {
          problems.push(`任务负责人「${ownerName || "（空）"}」未匹配到项目成员`);
        }
        const sd = normalizeDate(cell("startDate"));
        const ed = normalizeDate(cell("endDate"));
        if (!sd || !ed) {
          problems.push(`任务「${cell("taskName")}」缺少可识别的开始/截止日期`);
        }
        curKr.tasks.push({
          name: cell("taskName"),
          ownerId: ownerId ?? 0,
          startDate: sd,
          endDate: ed,
          expectedDeliverable: cell("deliverable") || undefined,
        });
      }
    }
    return { out, problems };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, mapping, nameOverrides]);

  const unmatched = personNames.filter((n) => !resolvePerson(n));

  const submit = async () => {
    setSaving(true);
    setError(null);
    const body: ImportRequest = {
      items: structure.out.map((o) => ({
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
    setSaving(false);
    if (res.data) {
      message.success(
        `已导入 ${res.data.objectives.length} 个 O、${res.data.tasks.length} 项任务草稿；请在全部任务页按 KR 批量提交入池`,
      );
      reset();
      onImported();
    } else {
      setError(res.error?.message ?? "导入失败");
    }
  };

  const memberOptions = members
    .filter((m) => m.role !== "viewer")
    .map((m) => ({ value: m.userId, label: `${m.displayName}（${m.username}）` }));

  return (
    <Modal
      title={
        <div>
          导入已有表格
          <span className="modal-sub">字段映射 → 人员匹配 → 结构预览 → 生成 O/KR 与任务草稿</span>
        </div>
      }
      open={open}
      width={860}
      onCancel={() => {
        reset();
        onClose();
      }}
      footer={null}
      destroyOnHidden
    >
      <Steps
        size="small"
        current={step}
        items={[{ title: "粘贴表格" }, { title: "字段映射" }, { title: "人员匹配" }, { title: "结构预览" }]}
        style={{ marginBottom: 16 }}
      />
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      {step === 0 && (
        <>
          <div className="notice" style={{ marginBottom: 10 }}>
            从 Excel／表格中复制内容后直接粘贴（首行为表头）；O、KR 列留空表示沿用上一行。
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
            <input
              type="file"
              accept=".csv,.tsv,.txt"
              onChange={async (e) => {
                const file = e.target.files?.[0];
                if (!file) return;
                // 读原始字节再自行解码：file.text() 固定按 UTF-8 且不剥 BOM（#97）。
                setRaw(decodeTable(await file.arrayBuffer()));
                setSourceFileName(file.name);
                setError(null);
              }}
            />
            <span className="muted">选择 CSV／TSV 文件，或直接粘贴到下方；文件名会随导入记录留存</span>
          </div>
          <Input.TextArea
            rows={10}
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            placeholder={"O 标题\tKR 描述\tKR 负责人\t任务名称\t任务负责人\t开始\t截止\t预期交付物"}
          />
          <div style={{ display: "flex", justifyContent: "flex-end", marginTop: 12 }}>
            <Button type="primary" onClick={goMapping}>
              下一步：字段映射
            </Button>
          </div>
        </>
      )}
      {step === 1 && (
        <>
          <div className="data-table-wrap" style={{ maxHeight: 320, overflow: "auto" }}>
            {/* 固定表格布局后列宽只由表头决定（#91）：每列给足映射下拉的宽度。 */}
            <table className="data-table" style={{ minWidth: columnCount * 156 }}>
              <thead>
                <tr>
                  {Array.from({ length: columnCount }, (_, i) => (
                    <th key={i} style={{ width: 156 }}>
                      <Select
                        size="small"
                        style={{ width: 140 }}
                        value={mapping[i]}
                        onChange={(v) =>
                          setMapping((m) => m.map((x, j) => (j === i ? v : x)))
                        }
                        options={(Object.keys(FIELD_LABEL) as FieldKey[]).map((k) => ({
                          value: k,
                          label: FIELD_LABEL[k],
                        }))}
                      />
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.slice(0, 6).map((r, ri) => (
                  <tr key={ri}>
                    {Array.from({ length: columnCount }, (_, ci) => (
                      <td key={ci} className={ri === 0 ? "muted" : ""} title={r[ci] ?? ""}>
                        {r[ci] ?? ""}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div style={{ display: "flex", justifyContent: "space-between", marginTop: 12 }}>
            <Button onClick={() => setStep(0)}>上一步</Button>
            <Button type="primary" onClick={() => setStep(2)}>
              下一步：人员匹配
            </Button>
          </div>
        </>
      )}
      {step === 2 && (
        <>
          {personNames.length === 0 && (
            <div className="empty compact-empty">表格中没有人员列</div>
          )}
          {personNames.map((n) => (
            <div key={n} className="fact-card fact-card-aux">
              <div>
                <b>{n}</b>
                <small>{memberByName.has(n) ? "已按姓名精确匹配" : "未匹配，请指定项目成员"}</small>
              </div>
              <Select
                style={{ width: 240 }}
                showSearch
                optionFilterProp="label"
                placeholder="选择项目成员"
                value={resolvePerson(n)}
                onChange={(v) => setNameOverrides((o) => ({ ...o, [n]: v }))}
                options={memberOptions}
              />
            </div>
          ))}
          <div style={{ display: "flex", justifyContent: "space-between", marginTop: 12 }}>
            <Button onClick={() => setStep(1)}>上一步</Button>
            <Button type="primary" disabled={unmatched.length > 0} onClick={() => setStep(3)}>
              下一步：结构预览
            </Button>
          </div>
        </>
      )}
      {step === 3 && (
        <>
          {structure.problems.length > 0 && (
            <Alert
              type="warning"
              style={{ marginBottom: 10 }}
              message={`存在 ${structure.problems.length} 个问题，修正后才能导入`}
              description={structure.problems.slice(0, 5).map((p, i) => (
                <div key={i}>{p}</div>
              ))}
            />
          )}
          {structure.out.map((o, oi) => (
            <div key={oi} style={{ marginBottom: 10 }}>
              <b>O{oi + 1} {o.title}</b>
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
                      任务：{tk.name} · {members.find((m) => m.userId === tk.ownerId)?.displayName ?? "？"} ·{" "}
                      {tk.startDate || "?"}—{tk.endDate || "?"}
                      {tk.expectedDeliverable ? ` · ${tk.expectedDeliverable}` : ""}
                    </div>
                  ))}
                </div>
              ))}
            </div>
          ))}
          <div className="notice">导入后任务为草稿；请到「全部任务」按 KR 勾选并批量提交入池审批。</div>
          <div style={{ display: "flex", justifyContent: "space-between", marginTop: 12 }}>
            <Button onClick={() => setStep(2)}>上一步</Button>
            <Button
              type="primary"
              loading={saving}
              disabled={structure.problems.length > 0 || structure.out.length === 0}
              onClick={submit}
            >
              确认导入
            </Button>
          </div>
        </>
      )}
    </Modal>
  );
}
