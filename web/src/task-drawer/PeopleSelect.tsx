import PersonPicker from "../PersonPicker";
import type { components } from "../api/schema";

type ProjectMember = components["schemas"]["ProjectMember"];

// #135／#166：参与人／成果审核人共用的就地多选——展开期间改草稿，收起时一次保存；
// 面板样式统一为人员选择组件（搜索框 + 头像行）。外部刷新（保存成功）后 value 变化会同步回草稿。
// 接收方因带「所有项目成员」哨兵逻辑单独实现（TaskDrawer 内）。
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
  return (
    <PersonPicker
      people={options.map((m) => ({
        userId: m.userId,
        displayName: m.displayName,
        username: m.username,
      }))}
      value={value}
      placeholder={placeholder}
      onSave={onSave}
    />
  );
}
