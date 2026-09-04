import type { ClipboardEvent } from "react";
import { Input } from "antd";
import type { PasswordProps } from "antd/es/input/Password";

// 全系统共用的密码输入框（模块 PRD §3.3；#203／#209）：带显示／隐藏切换，
// 拦截复制与剪切（明文态下浏览器默认可复制，须显式拦截），保留粘贴。
const block = (e: ClipboardEvent<HTMLInputElement>) => e.preventDefault();

export default function PasswordInput(props: PasswordProps) {
  return <Input.Password {...props} onCopy={block} onCut={block} />;
}
