import { createContext, useCallback, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { client } from "./api/client";
import type { components } from "./api/schema";

type Branding = components["schemas"]["Branding"];

// 品牌信息（#210）：登录页与两套壳的名称、副标题、提示语都读系统设置；接口免登录，
// App 启动时拉一次，系统管理员改完基本信息后调用 reload 即时刷新；浏览器标签页标题跟随系统名称。
const FALLBACK: Branding = {
  systemName: "协同管理工具",
  subtitle: "O／KR／任务协同推进",
  loginHint: "账号由管理员分配",
  canRecoverPassword: false,
};

const BrandingContext = createContext<{ branding: Branding; reload: () => Promise<void> }>({
  branding: FALLBACK,
  reload: async () => {},
});

export function BrandingProvider({ children }: { children: ReactNode }) {
  const [branding, setBranding] = useState<Branding>(FALLBACK);
  const reload = useCallback(async () => {
    const { data } = await client.GET("/branding");
    if (data) setBranding(data);
  }, []);
  useEffect(() => {
    reload();
  }, [reload]);
  useEffect(() => {
    document.title = branding.systemName;
  }, [branding.systemName]);
  // favicon 跟随 logo（#211）：未上传时保持 index.html 的内联图标。
  useEffect(() => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) return;
    if (!link.dataset.defaultHref) link.dataset.defaultHref = link.href;
    link.href = logoUrl(branding) ?? link.dataset.defaultHref;
  }, [branding]);
  return <BrandingContext.Provider value={{ branding, reload }}>{children}</BrandingContext.Provider>;
}

export function useBranding() {
  return useContext(BrandingContext);
}

// logo 出图地址（#211）：经后端流式读取，URL 带版本号让换图后浏览器拿到新图；未上传为空。
export function logoUrl(b: Branding): string | null {
  return b.logoVersion ? `/api/v1/branding/logo?v=${b.logoVersion}` : null;
}
