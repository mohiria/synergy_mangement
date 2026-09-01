import { execFileSync } from "node:child_process";

// 冒烟前重建演示数据：断言要的是固定坐标（编号、任务名、已完成任务的分布），
// 不重建就会被上一次跑留下的状态改写。cmd/seed 会清空全部业务数据，只在开发库上跑。
export default function globalSetup() {
  if (process.env.E2E_SKIP_SEED === "1") return;
  if (!process.env.DATABASE_URL) {
    throw new Error("e2e 需要 DATABASE_URL（或用 E2E_SKIP_SEED=1 跳过重建演示数据）");
  }
  if (!process.env.SEED_PASSWORD) {
    throw new Error("e2e 需要 SEED_PASSWORD：演示账号口令不入库，也不写进仓库");
  }
  execFileSync("go", ["run", "./cmd/seed", "-skip-files"], {
    cwd: "../server",
    stdio: "inherit",
    env: process.env,
  });
}
