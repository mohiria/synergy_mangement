import { DatePicker } from "antd";
import type { Dayjs } from "dayjs";

// 统一日期区间组件（AC-52）：一次完成开始／截止选择。
// 控件内不重复「开始／结束」标题（§7.2）——区间语义由外层字段名（任务周期、KR 周期、计划周期）承担，
// 占位只提示要填的是一个日期，用中文写（#96），不写英文格式串、也不写「开始／结束」。
export type DateRange = [Dayjs | null, Dayjs | null] | null;

export default function DateRangeField({
  value,
  onChange,
  allowEmpty = false,
  disabled,
  open,
  onOpenChange,
  autoFocus,
  "aria-label": ariaLabel,
}: {
  value?: DateRange;
  onChange?: (value: DateRange) => void;
  allowEmpty?: boolean;
  disabled?: boolean;
  // #219：设置页「点击即编辑」需要进入编辑态就展开面板、面板关闭时退出编辑态。
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  autoFocus?: boolean;
  "aria-label"?: string;
}) {
  return (
    <DatePicker.RangePicker
      className="date-range-field"
      style={{ width: "100%" }}
      value={value ?? undefined}
      onChange={(v) => onChange?.(v ?? null)}
      allowEmpty={allowEmpty ? [true, true] : undefined}
      disabled={disabled}
      open={open}
      onOpenChange={onOpenChange}
      autoFocus={autoFocus}
      aria-label={ariaLabel}
      separator="—"
      placeholder={["年-月-日", "年-月-日"]}
    />
  );
}
