import { useEffect, useMemo, useRef, useState } from "react";
import { Input, Popover } from "antd";
import type { InputRef } from "antd";

// 人员选择组件（#166，按 references/人员选择组件参考.png 统一）：
// 点击触发区弹出面板——顶部搜索框 + 成员列表，每行「头像 + 姓名 + 账号」，点击选中。
// 多选保持「展开期间改草稿、收起时一次保存」的既有交互（#135 同口径）；
// 参考图底部的「配置权限」类入口不做。搜索按姓名或账号过滤。

export type PickerPerson = {
  userId: number;
  displayName: string;
  username?: string;
};

// 头像底色按人取固定色（同一人在各处颜色一致），文案取姓名末字更易识别（中文姓名习惯）。
const AVATAR_COLORS = [
  ["#e8edff", "#4055c7"],
  ["#e6f6ee", "#1f9d63"],
  ["#fff2e3", "#c07a1b"],
  ["#fdeaea", "#c24646"],
  ["#eef3e6", "#5f8f2f"],
  ["#eae6fb", "#6a4fc7"],
  ["#e3f4f8", "#1f8ba6"],
];

export function PersonAvatar({ person, size = 26 }: { person: PickerPerson; size?: number }) {
  const [bg, fg] = AVATAR_COLORS[Math.abs(Number(person.userId)) % AVATAR_COLORS.length];
  const name = person.displayName.trim();
  const text = name.length > 1 ? name.slice(-1) : name;
  return (
    <span
      className="avatar"
      style={{ width: size, height: size, background: bg, color: fg }}
      aria-hidden
    >
      {text}
    </span>
  );
}

function PersonRow({
  person,
  selected,
  onClick,
}: {
  person: PickerPerson;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <div
      className={`pp-row${selected ? " selected" : ""}`}
      onClick={onClick}
      role="option"
      aria-selected={selected}
    >
      <PersonAvatar person={person} />
      <span className="pp-text">
        <span className="pp-name">{person.displayName}</span>
        {person.username ? <span className="pp-username">（{person.username}）</span> : null}
      </span>
      {selected && <span className="pp-check">✓</span>}
    </div>
  );
}

export default function PersonPicker({
  people,
  value,
  multiple = true,
  placeholder,
  disabled = false,
  displayText,
  normalizeDraft,
  onSave,
  size = "small",
}: {
  people: PickerPerson[];
  value: number[];
  multiple?: boolean;
  placeholder: string;
  disabled?: boolean;
  /** 触发区显示文案；缺省按已选姓名拼接。 */
  displayText?: string;
  /** 草稿变化钩子（哨兵互斥等归调用方定义）。 */
  normalizeDraft?: (next: number[], prev: number[]) => number[];
  /** 面板收起时一次保存（多选）；单选在点击行时立即触发并收起。 */
  onSave: (ids: number[]) => void;
  size?: "small" | "middle";
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<number[]>(value);
  const [search, setSearch] = useState("");
  const searchRef = useRef<InputRef>(null);

  // 外部刷新（保存成功）后 value 变化同步回草稿（#135 同口径）。
  useEffect(() => {
    if (!open) setDraft(value);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value.join(",")]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return people;
    return people.filter(
      (p) =>
        p.displayName.toLowerCase().includes(q) || (p.username ?? "").toLowerCase().includes(q),
    );
  }, [people, search]);

  const byId = useMemo(() => new Map(people.map((p) => [p.userId, p])), [people]);
  const label =
    displayText ??
    value
      .map((id) => byId.get(id)?.displayName ?? "")
      .filter(Boolean)
      .join("、");

  const toggle = (id: number) => {
    if (!multiple) {
      setOpen(false);
      setSearch("");
      if (value.length !== 1 || value[0] !== id) onSave([id]);
      return;
    }
    setDraft((prev) => {
      const next = prev.includes(id) ? prev.filter((v) => v !== id) : [...prev, id];
      return normalizeDraft ? normalizeDraft(next, prev) : next;
    });
  };

  const handleOpenChange = (o: boolean) => {
    if (disabled) return;
    setOpen(o);
    if (o) {
      setDraft(value);
      setSearch("");
      setTimeout(() => searchRef.current?.focus(), 0);
    } else {
      setSearch("");
      if (multiple && [...draft].sort().join(",") !== [...value].sort().join(",")) {
        onSave(draft);
      }
    }
  };

  const selectedSet = new Set(multiple ? draft : value);
  return (
    <Popover
      open={open}
      onOpenChange={handleOpenChange}
      trigger="click"
      placement="bottomLeft"
      arrow={false}
      styles={{ body: { padding: 0 } }}
      content={
        <div className="pp-panel" role="listbox" aria-multiselectable={multiple}>
          <div className="pp-search">
            <Input
              ref={searchRef}
              allowClear
              size="middle"
              placeholder="搜索姓名或账号"
              prefix={<span aria-hidden>🔍</span>}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          <div className="pp-list">
            {filtered.length === 0 && <div className="pp-empty">没有匹配的成员</div>}
            {filtered.map((p) => (
              <PersonRow
                key={p.userId}
                person={p}
                selected={selectedSet.has(p.userId)}
                onClick={() => toggle(p.userId)}
              />
            ))}
          </div>
        </div>
      }
    >
      <button
        type="button"
        className={`pp-trigger${disabled ? " disabled" : ""}${size === "middle" ? " middle" : ""}`}
        disabled={disabled}
      >
        {label ? (
          <span className="pp-trigger-text">{label}</span>
        ) : (
          <span className="pp-trigger-text muted">{placeholder}</span>
        )}
        <span className="pp-caret" aria-hidden>
          ▾
        </span>
      </button>
    </Popover>
  );
}
