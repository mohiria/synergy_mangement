import createClient from "openapi-fetch";
import type { paths } from "./schema";

// 唯一 API 客户端：类型来自仓库根 openapi.yaml（npm run gen:api 重新生成）。
// 界面反馈只消费 API 派生字段，前端不复刻业务规则。
export const client = createClient<paths>({ baseUrl: "/api/v1" });
