import type { ReactNode } from "react";
import { logoUrl, useBranding } from "./branding";

// 品牌块（#198）：登录页与两套壳侧栏共用同一份「标志 + 系统名称 + 副标题」。
// #210 起名称与副标题默认读系统设置（BrandingProvider），仍可用 props 覆盖；侧栏一行放不下时省略号截断（ui.css）。
export const DEFAULT_SYSTEM_NAME = "协同管理工具";
export const DEFAULT_SUBTITLE = "O／KR／任务协同推进";

export function Brand({
  name,
  subtitle,
  mark,
  className = "brand",
}: {
  name?: string;
  subtitle?: string;
  // 标志：默认取系统名称首字；将来上传 logo 后传图片节点（#211）。
  mark?: ReactNode;
  // 登录页用 login-brand，侧栏用 brand；样式契约在 ui.css。
  className?: string;
}) {
  const { branding } = useBranding();
  const shownName = name ?? branding.systemName ?? DEFAULT_SYSTEM_NAME;
  const shownSubtitle = subtitle ?? branding.subtitle ?? DEFAULT_SUBTITLE;
  // #211：上传了 logo 就显示图片（非正方形居中裁切，见 ui.css），否则回退系统名称首字。
  const logo = logoUrl(branding);
  const shownMark = mark ?? (logo ? <img src={logo} alt="" /> : shownName.slice(0, 1));
  return (
    <div className={className}>
      <span className="brand-mark">{shownMark}</span>
      <div className="brand-name">
        <b title={shownName}>{shownName}</b>
        {shownSubtitle && <span title={shownSubtitle}>{shownSubtitle}</span>}
      </div>
    </div>
  );
}
