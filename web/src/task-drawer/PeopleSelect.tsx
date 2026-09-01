import { useEffect, useState } from "react";
import { Select } from "antd";
import type { components } from "../api/schema";

type ProjectMember = components["schemas"]["ProjectMember"];

// #135：参与人／成果审核人共用的就地多选下拉——展开期间改草稿，收起时一次保存，
// 不再走弹窗；选项一行「头像 姓名（用户名）」（#126 风格）。外部刷新（保存成功）后
// value 变化会同步回草稿。接收方因带「所有项目成员」哨兵逻辑单独实现（TaskDrawer 内）。
export default function PeopleSelect({
  value,
  options,
  placeholder,
  onSave,
}: {
  value: number[];
  options: ProjectMember[];
  placeholder: string;
  onSave: (ids: number[]) => void;
}) {
  const [draft, setDraft] = useState<number[]>(value);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (!open) setDraft(value);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value.join(",")]);
  return (
    <Select
      mode="multiple"
      size="small"
      style={{ minWidth: 220, maxWidth: 420, flex: 1 }}
      placeholder={placeholder}
      value={draft}
      onChange={(ids: number[]) => setDraft(ids)}
      onDropdownVisibleChange={(o) => {
        setOpen(o);
        if (!o && [...draft].sort().join(",") !== [...value].sort().join(",")) {
          onSave(draft);
        }
      }}
      optionFilterProp="label"
      options={options.map((m) => ({
        value: m.userId,
        label: `${m.displayName}（${m.username}）`,
      }))}
      optionRender={(opt) => {
        const m = options.find((c) => c.userId === opt.value);
        if (!m) return opt.label;
        return (
          <span className="owner-cell">
            <span className="avatar">{m.displayName.slice(0, 1)}</span>
            <span className="cell-text">
              {m.displayName}（{m.username}）
            </span>
          </span>
        );
      }}
    />
  );
}
