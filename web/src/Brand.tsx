import type { ReactNode } from "react";

// 品牌块（#198）：登录页与两套壳侧栏共用同一份「标志 + 系统名称 + 副标题」。
// 数据来源暂为常量默认值；#210／#211 起改读系统设置，只需改这一处。
export const DEFAULT_SYSTEM_NAME = "协同管理工具";
export const DEFAULT_SUBTITLE = "O／KR／任务协同推进";

export function Brand({
  name = DEFAULT_SYSTEM_NAME,
  subtitle = DEFAULT_SUBTITLE,
  mark,
  className = "brand",
}: {
  name?: string;
  subtitle?: string;
  // 标志：默认取系统名称首字；将来上传 logo 后传图片节点。
  mark?: ReactNode;
  // 登录页用 login-brand，侧栏用 brand；样式契约在 ui.css。
  className?: string;
}) {
  return (
    <div className={className}>
      <span className="brand-mark">{mark ?? name.slice(0, 1)}</span>
      <div className="brand-name">
        <b>{name}</b>
        <span>{subtitle}</span>
      </div>
    </div>
  );
}
