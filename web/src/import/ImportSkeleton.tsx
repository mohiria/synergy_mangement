import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Alert, Button, Input, Modal, Select, Steps, message } from "antd";
import type { components } from "../api/schema";
import FileUploadField from "../FileUploadField";
import { parseDelimited, parseFile } from "./parseTable";
import { downloadTemplate, type TemplateColumn } from "./template";

type ProjectMember = components["schemas"]["ProjectMember"];

// 导入器骨架（#105）：步骤条、上传／粘贴、字段映射、人员匹配、结构预览与提交都在这里，
// 业务只通过参数注入——字段集合、表头猜测、结构组装、预览与提交端点。
// 骨架不认识 O／KR／任务，也不含任何业务规则；规则一律在服务端。

export type ImportField<K extends string> = {
  key: K;
  label: string;
  /** 表头猜测：命中即把该列映射到本字段，按数组顺序先到先得 */
  guess?: RegExp;
  /** 该列是人名列，进入「人员匹配」步骤 */
  person?: boolean;
};

/** 结构组装的入参：已按映射取好列的取值器与人名解析器。 */
export type AssembleContext<K extends string> = {
  /** 数据行（不含表头） */
  rows: string[][];
  cell: (row: string[], key: K) => string;
  resolvePerson: (name: string) => number | undefined;
  memberName: (userId?: number) => string;
};

export type ImportSkeletonProps<K extends string, S> = {
  open: boolean;
  title: string;
  subtitle: string;
  /** 第一步的说明条 */
  intro: string;
  pastePlaceholder: string;
  templateFileName: string;
  templateColumns: TemplateColumn[];
  fields: ImportField<K>[];
  members: ProjectMember[];
  assemble: (ctx: AssembleContext<K>) => { structure: S; problems: string[] };
  renderPreview: (structure: S) => ReactNode;
  isEmpty: (structure: S) => boolean;
  /** 提交成功返回 null，失败返回要展示的错误文案 */
  submit: (structure: S, sourceFileName: string) => Promise<string | null>;
  successMessage: (structure: S) => string;
  previewNote?: ReactNode;
  width?: number;
  onClose: () => void;
  onDone: () => void;
};

const IGNORE = "ignore";

export default function ImportSkeleton<K extends string, S>({
  open,
  title,
  subtitle,
  intro,
  pastePlaceholder,
  templateFileName,
  templateColumns,
  fields,
  members,
  assemble,
  renderPreview,
  isEmpty,
  submit,
  successMessage,
  previewNote,
  width = 860,
  onClose,
  onDone,
}: ImportSkeletonProps<K, S>) {
  const [step, setStep] = useState(0);
  const [raw, setRaw] = useState("");
  const [fileRows, setFileRows] = useState<string[][] | null>(null);
  const [mapping, setMapping] = useState<string[]>([]);
  const [nameOverrides, setNameOverrides] = useState<Record<string, number>>({});
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  // 源文件名随导入记录留存（§7.9、AC-68）：选文件时自动带上，粘贴时留空。
  const [sourceFileName, setSourceFileName] = useState("");
  const [picked, setPicked] = useState<File | null>(null);

  const reset = () => {
    setStep(0);
    setRaw("");
    setFileRows(null);
    setPicked(null);
    setSourceFileName("");
    setMapping([]);
    setNameOverrides({});
    setError(null);
  };
  useEffect(() => {
    if (!open) reset();
  }, [open]);

  // 选了文件就用文件的解析结果，否则用粘贴文本。
  const rows = useMemo(
    () => fileRows ?? (raw.trim() === "" ? [] : parseDelimited(raw)),
    [fileRows, raw],
  );
  const columnCount = rows.reduce((n, r) => Math.max(n, r.length), 0);

  const fieldOptions = [
    { value: IGNORE, label: "忽略此列" },
    ...fields.map((f) => ({ value: f.key as string, label: f.label })),
  ];

  const goMapping = () => {
    if (rows.length === 0) {
      setError("请先选择文件或粘贴表格内容（首行为表头）");
      return;
    }
    setError(null);
    const header = rows[0];
    const guess = (h: string) => fields.find((f) => f.guess?.test(h))?.key ?? IGNORE;
    setMapping(Array.from({ length: columnCount }, (_, i) => guess(header[i] ?? "")));
    setStep(1);
  };

  const pickFile = async (file: File | null) => {
    setPicked(file);
    if (!file) {
      setFileRows(null);
      setSourceFileName("");
      return;
    }
    try {
      setFileRows(await parseFile(file));
      setSourceFileName(file.name);
      setError(null);
    } catch {
      setFileRows(null);
      setError("这个文件读不出表格内容，请确认是 CSV 或 xlsx");
    }
  };

  // 人员匹配：姓名 → 成员（精确匹配 displayName，否则待人工指定）。
  const memberByName = useMemo(() => {
    const m = new Map<string, number>();
    members.forEach((mm) => m.set(mm.displayName, mm.userId));
    return m;
  }, [members]);
  const resolvePerson = (name: string): number | undefined =>
    nameOverrides[name] ?? memberByName.get(name);
  const memberName = (userId?: number) =>
    members.find((m) => m.userId === userId)?.displayName ?? "";

  const dataRows = rows.slice(1);
  const cell = (row: string[], key: K) => {
    const c = mapping.indexOf(key as string);
    return c >= 0 ? (row[c] ?? "") : "";
  };

  const personNames = useMemo(() => {
    const names = new Set<string>();
    const keys = fields.filter((f) => f.person).map((f) => f.key as string);
    for (const r of dataRows) {
      for (const key of keys) {
        const c = mapping.indexOf(key);
        if (c >= 0 && r[c]) names.add(r[c]);
      }
    }
    return [...names];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, mapping]);

  const assembled = useMemo(
    () => assemble({ rows: dataRows, cell, resolvePerson, memberName }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [rows, mapping, nameOverrides],
  );
  const unmatched = personNames.filter((n) => !resolvePerson(n));

  const doSubmit = async () => {
    setSaving(true);
    setError(null);
    const err = await submit(assembled.structure, sourceFileName);
    setSaving(false);
    if (err) {
      setError(err);
      return;
    }
    message.success(successMessage(assembled.structure));
    reset();
    onDone();
  };

  const memberOptions = members
    .filter((m) => m.role !== "viewer")
    .map((m) => ({ value: m.userId, label: `${m.displayName}（${m.username}）` }));

  return (
    <Modal
      title={
        <div>
          {title}
          <span className="modal-sub">{subtitle}</span>
        </div>
      }
      open={open}
      width={width}
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
        items={[{ title: "选择表格" }, { title: "字段映射" }, { title: "人员匹配" }, { title: "结构预览" }]}
        style={{ marginBottom: 16 }}
      />
      {error && <Alert type="error" message={error} style={{ marginBottom: 12 }} />}
      {step === 0 && (
        <>
          <div className="notice" style={{ marginBottom: 10 }}>{intro}</div>
          {/* 统一上传组件（AC-52）：全中文，替掉浏览器自带的英文 file 按钮（#105）。 */}
          <FileUploadField
            value={picked}
            onChange={pickFile}
            accept=".csv,.xlsx"
            prompt="点击选择或将表格拖到此处（CSV／xlsx）"
            hint="文件名会随导入记录留存；也可以直接把表格粘贴到下方"
          />
          <div style={{ display: "flex", alignItems: "center", gap: 8, margin: "10px 0 8px" }}>
            <Button size="small" onClick={() => downloadTemplate(templateFileName, templateColumns)}>
              下载 xlsx 模板
            </Button>
            <span className="muted">模板表头与本导入器的字段一致，可直接填写后上传</span>
          </div>
          <Input.TextArea
            rows={8}
            value={raw}
            onChange={(e) => {
              setRaw(e.target.value);
              setFileRows(null);
              setPicked(null);
              setSourceFileName("");
            }}
            placeholder={pastePlaceholder}
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
                        onChange={(v) => setMapping((m) => m.map((x, j) => (j === i ? v : x)))}
                        options={fieldOptions}
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
          {personNames.length === 0 && <div className="empty compact-empty">表格中没有人员列</div>}
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
          {assembled.problems.length > 0 && (
            <Alert
              type="warning"
              style={{ marginBottom: 10 }}
              message={`存在 ${assembled.problems.length} 个问题，修正后才能导入`}
              description={assembled.problems.slice(0, 5).map((p, i) => (
                <div key={i}>{p}</div>
              ))}
            />
          )}
          {renderPreview(assembled.structure)}
          {previewNote}
          <div style={{ display: "flex", justifyContent: "space-between", marginTop: 12 }}>
            <Button onClick={() => setStep(2)}>上一步</Button>
            <Button
              type="primary"
              loading={saving}
              disabled={assembled.problems.length > 0 || isEmpty(assembled.structure)}
              onClick={doSubmit}
            >
              确认导入
            </Button>
          </div>
        </>
      )}
    </Modal>
  );
}
