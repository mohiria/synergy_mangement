import { useCallback, useRef, useState, type ReactNode } from "react";
import { Input, InputNumber, Select } from "antd";

// 设置页字段的「查看／编辑」外壳（#219）：与任务抽屉「任务概览」同一套机制——默认只显示值，
// 有权限时悬停提示「点击编辑」，点击后换成控件并自动聚焦；改完即存，无保存按钮。
// 保存回调返回错误文案（null 表示成功）：失败留在编辑态并就地提示；校验不过不发请求。
// 权限只消费后端派生字段（canEdit），这里不判断角色。

export type InlineSave<T> = (value: T) => Promise<string | null>;

// 逐字段保存是整表 PUT，连续改两个字段时第二次请求必须等第一次返回、再从最新对象拼 body，
// 否则会用旧快照把前一次改动冲掉（PR #220 review）。调用方把保存动作交给这里排队；
// 前一次失败不影响后一次执行。
export function useSaveQueue() {
  const queue = useRef<Promise<unknown>>(Promise.resolve());
  return useCallback(<T,>(task: () => Promise<T>): Promise<T> => {
    const run = queue.current.then(task, task);
    queue.current = run.catch(() => undefined);
    return run;
  }, []);
}

export function InlineField({
  label,
  canEdit,
  value,
  editing,
  onBeginEdit,
  children,
}: {
  label: string;
  canEdit: boolean;
  /** 查看态显示的值；空则显示「—」。 */
  value: ReactNode;
  editing: boolean;
  onBeginEdit: () => void;
  /** 编辑态控件。 */
  children: ReactNode;
}) {
  if (editing) return <>{children}</>;
  const empty = value === null || value === undefined || value === "";
  return (
    <div
      className={`settings-value${canEdit ? " inline-editable" : ""}`}
      aria-label={label}
      role={canEdit ? "button" : undefined}
      tabIndex={canEdit ? 0 : undefined}
      title={canEdit ? "点击编辑" : undefined}
      onClick={canEdit ? onBeginEdit : undefined}
      onKeyDown={
        canEdit
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onBeginEdit();
              }
            }
          : undefined
      }
    >
      {empty ? <span className="muted">—</span> : value}
    </div>
  );
}

// 文本字段：回车或失焦即存，Esc 放弃；值未变直接退出。
export function InlineText({
  label,
  canEdit,
  value,
  maxLength,
  placeholder,
  showCount,
  validate,
  onSave,
}: {
  label: string;
  canEdit: boolean;
  value: string;
  maxLength?: number;
  placeholder?: string;
  showCount?: boolean;
  validate?: (value: string) => string | null;
  onSave: InlineSave<string>;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [error, setError] = useState<string | null>(null);
  const draftRef = useRef(value);
  const skipBlurRef = useRef(false);
  // 请求期间输入框置为 disabled：失焦后控件仍挂着，不禁用的话用户可以再点进去改草稿，
  // 而前一次保存成功会无条件退出编辑态，把新草稿静默丢掉（PR #220 review）。
  const [saving, setSaving] = useState(false);

  const begin = () => {
    draftRef.current = value;
    setDraft(value);
    setError(null);
    // Esc 后被移除的输入框未必再触发 blur，标记要在进入编辑态时清掉，否则下次首个回车／失焦会被吞掉。
    skipBlurRef.current = false;
    setEditing(true);
  };
  const commit = async () => {
    if (skipBlurRef.current) {
      skipBlurRef.current = false;
      return;
    }
    if (saving) return;
    const next = draftRef.current.trim();
    if (next === value) {
      setEditing(false);
      return;
    }
    const invalid = validate?.(next) ?? null;
    if (invalid) {
      setError(invalid);
      return;
    }
    setSaving(true);
    const err = await onSave(next);
    setSaving(false);
    if (err) setError(err);
    else setEditing(false);
  };
  return (
    <InlineField label={label} canEdit={canEdit} value={value} editing={editing} onBeginEdit={begin}>
      <Input
        autoFocus
        disabled={saving}
        value={draft}
        maxLength={maxLength}
        placeholder={placeholder}
        showCount={showCount}
        status={error ? "error" : undefined}
        aria-label={label}
        onChange={(e) => {
          draftRef.current = e.target.value;
          setDraft(e.target.value);
          if (error) setError(null);
        }}
        onPressEnter={(e) => (e.target as HTMLInputElement).blur()}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            skipBlurRef.current = true;
            setEditing(false);
          }
        }}
      />
      {error && <div className="inline-field-error">{error}</div>}
    </InlineField>
  );
}

// 下拉字段：进入编辑态自动展开，选中即存；下拉关闭（未选）退出编辑态。
export function InlineSelect<T extends string>({
  label,
  canEdit,
  value,
  display,
  options,
  onSave,
}: {
  label: string;
  canEdit: boolean;
  value: T;
  /** 查看态文案：已有对象一律取后端派生的 label。 */
  display: ReactNode;
  options: { value: T; label: string }[];
  onSave: InlineSave<T>;
}) {
  const [editing, setEditing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // 请求期间下拉置为 disabled：受控 Select 在 onSave 返回前仍显示旧值，不禁用的话用户可以再开再选，
  // 「改回原值」会被相等分支吞掉、而前一次请求随后把新值落库（PR #220 review）。
  const [saving, setSaving] = useState(false);
  const pickedRef = useRef(false);
  const begin = () => {
    pickedRef.current = false;
    setError(null);
    setEditing(true);
  };
  return (
    <InlineField label={label} canEdit={canEdit} value={display} editing={editing} onBeginEdit={begin}>
      <Select<T>
        autoFocus
        defaultOpen
        disabled={saving}
        value={value}
        options={options}
        status={error ? "error" : undefined}
        style={{ width: 240 }}
        aria-label={label}
        onChange={async (v) => {
          pickedRef.current = true;
          if (v === value) {
            setEditing(false);
            return;
          }
          setSaving(true);
          const err = await onSave(v);
          setSaving(false);
          if (err) setError(err);
          else setEditing(false);
        }}
        onOpenChange={(open) => {
          if (!open && !pickedRef.current) setEditing(false);
        }}
      />
      {error && <div className="inline-field-error">{error}</div>}
    </InlineField>
  );
}

// 整数字段：回车或失焦即存，范围外不发请求。
export function InlineNumber({
  label,
  canEdit,
  value,
  min,
  max,
  suffix,
  onSave,
}: {
  label: string;
  canEdit: boolean;
  value: number;
  min: number;
  max: number;
  suffix: string;
  onSave: InlineSave<number>;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<number | null>(value);
  const [error, setError] = useState<string | null>(null);
  const draftRef = useRef<number | null>(value);
  const skipBlurRef = useRef(false);
  // 请求期间输入框置为 disabled：失焦后控件仍挂着，不禁用的话用户可以再点进去改草稿，
  // 而前一次保存成功会无条件退出编辑态，把新草稿静默丢掉（PR #220 review）。
  const [saving, setSaving] = useState(false);
  const begin = () => {
    draftRef.current = value;
    setDraft(value);
    setError(null);
    // Esc 后被移除的输入框未必再触发 blur，标记要在进入编辑态时清掉，否则下次首个回车／失焦会被吞掉。
    skipBlurRef.current = false;
    setEditing(true);
  };
  const commit = async () => {
    if (skipBlurRef.current) {
      skipBlurRef.current = false;
      return;
    }
    if (saving) return;
    const next = draftRef.current;
    if (next === value) {
      setEditing(false);
      return;
    }
    if (next === null || !Number.isInteger(next) || next < min || next > max) {
      setError(`请输入 ${min}～${max} 之间的整数`);
      return;
    }
    setSaving(true);
    const err = await onSave(next);
    setSaving(false);
    if (err) setError(err);
    else setEditing(false);
  };
  return (
    <InlineField
      label={label}
      canEdit={canEdit}
      value={`${value} ${suffix}`}
      editing={editing}
      onBeginEdit={begin}
    >
      <InputNumber
        autoFocus
        disabled={saving}
        min={min}
        max={max}
        precision={0}
        value={draft}
        addonAfter={suffix}
        status={error ? "error" : undefined}
        style={{ width: 200 }}
        aria-label={label}
        onChange={(v) => {
          draftRef.current = v;
          setDraft(v);
          if (error) setError(null);
        }}
        onPressEnter={(e) => (e.target as HTMLInputElement).blur()}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            skipBlurRef.current = true;
            setEditing(false);
          }
        }}
      />
      {error && <div className="inline-field-error">{error}</div>}
    </InlineField>
  );
}
