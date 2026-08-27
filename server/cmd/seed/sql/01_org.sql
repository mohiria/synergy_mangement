-- 演示数据集：清空全部业务数据后重建一套可用于演示与联调的完整数据。
--
-- 约定：
--   1. 所有时间都相对 now()／current_date 计算，任何一天跑都能得到同样的派生结果
--      （超期、审批超时、上游未就绪等卡点在种子里是被有意造出来的）；
--   2. 显式写 id（OVERRIDING SYSTEM VALUE），identity 序列在 99_sequences.sql 里统一重置；
--   3. 只写库里真实存在的事实，派生字段（显示状态、卡点、我的工作分组）交给 domain 层读时计算：
--      因此没有 waiting_input 这个存储状态，等待输入由「必要输入未就绪」派生。
--
-- 场景：某集团数字化中心（约 100 人）在跑的四个项目。
-- 本目录下的文件按文件名顺序执行（go run ./cmd/seed 会在一个事务里跑完）。

-- 口令哈希用 pgcrypto 的 bcrypt 现算（$2a$10$，与 golang.org/x/crypto/bcrypt 兼容），
-- 使本文件可以脱离 Go 直接用 psql -f 执行。
CREATE EXTENSION IF NOT EXISTS pgcrypto;

TRUNCATE TABLE
    sessions, notifications, discussion_mentions, discussions,
    task_activities, remind_logs,
    artifact_package_items, artifact_packages,
    completion_review_items, completion_review_reviewers, completion_reviews,
    field_change_requests, input_requests, deliverable_edges,
    deliverable_files, deliverables,
    task_receipts, task_receivers, task_reviewers, task_invites,
    pool_reviews, tasks, key_results, objectives,
    project_members, projects, users
    RESTART IDENTITY CASCADE;

-- ── 用户 ──────────────────────────────────────────────────────────────────────
-- 口令统一为 synergy@2026（bcrypt cost 10）。
INSERT INTO users (id, username, display_name, password_hash, created_at)
OVERRIDING SYSTEM VALUE VALUES
    (1,  'zhaowenqi',   '赵文琪', crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '400 days'),
    (2,  'lijianhui',   '李建辉', crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '398 days'),
    (3,  'chenmuyang',  '陈牧阳', crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '395 days'),
    (4,  'wanghaoran',  '王浩然', crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '395 days'),
    (5,  'liuxinyi',    '刘欣怡', crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '380 days'),
    (6,  'sunpeng',     '孙鹏',   crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '372 days'),
    (7,  'zhoujiaqi',   '周佳琪', crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '365 days'),
    (8,  'wuyufan',     '吴雨凡', crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '333 days'),
    (9,  'zhengkai',    '郑凯',   crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '330 days'),
    (10, 'hanmeng',     '韩萌',   crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '300 days'),
    (11, 'xushuai',     '徐帅',   crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '288 days'),
    (12, 'guoting',     '郭婷',   crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '260 days'),
    (13, 'maozhicheng', '毛志成', crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '210 days'),
    (14, 'linxiaoyu',   '林小雨', crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '188 days'),
    (15, 'hejing',      '何静',   crypt('synergy@2026', gen_salt('bf', 10)), now() - interval '150 days');

-- ── 项目 ──────────────────────────────────────────────────────────────────────
INSERT INTO projects (id, name, created_by, owner_id, status, stage, planned_start_date, planned_end_date, created_at)
OVERRIDING SYSTEM VALUE VALUES
    (1, '核心业务系统数据库国产化迁移', 2, 1,  'in_progress', '试点割接准备', current_date - 180, current_date + 96,  now() - interval '182 days'),
    (2, '客户服务中台一期建设',         2, 2,  'in_progress', '联调与 UAT',   current_date - 110, current_date + 114, now() - interval '112 days'),
    (3, '智能客服知识库试点',           7, 7,  'in_progress', '试运行',       current_date - 52,  current_date + 65,  now() - interval '54 days'),
    (4, '等保 2.0 三级测评整改',        1, 11, 'completed',   '已通过复测',   current_date - 232, current_date - 57,  now() - interval '236 days');

-- ── 项目成员 ──────────────────────────────────────────────────────────────────
INSERT INTO project_members (project_id, user_id, role, created_at) VALUES
    -- 迁移项目：数字化中心 + DBA + 安全 + 集成商
    (1, 1,  'admin',  now() - interval '182 days'),
    (1, 2,  'admin',  now() - interval '182 days'),
    (1, 3,  'member', now() - interval '181 days'),
    (1, 4,  'member', now() - interval '181 days'),
    (1, 5,  'member', now() - interval '160 days'),
    (1, 6,  'member', now() - interval '178 days'),
    (1, 9,  'member', now() - interval '181 days'),
    (1, 11, 'member', now() - interval '175 days'),
    (1, 13, 'member', now() - interval '140 days'),
    (1, 15, 'viewer', now() - interval '138 days'),
    -- 客服中台：产品 + 研发 + 业务 + 数据
    (2, 1,  'viewer', now() - interval '110 days'),
    (2, 2,  'admin',  now() - interval '112 days'),
    (2, 7,  'admin',  now() - interval '112 days'),
    (2, 3,  'member', now() - interval '110 days'),
    (2, 4,  'member', now() - interval '110 days'),
    (2, 5,  'member', now() - interval '108 days'),
    (2, 6,  'member', now() - interval '105 days'),
    (2, 8,  'member', now() - interval '100 days'),
    (2, 10, 'member', now() - interval '100 days'),
    (2, 11, 'member', now() - interval '88 days'),
    (2, 12, 'member', now() - interval '112 days'),
    (2, 14, 'member', now() - interval '90 days'),
    -- 知识库试点：小团队
    (3, 7,  'admin',  now() - interval '54 days'),
    (3, 12, 'admin',  now() - interval '54 days'),
    (3, 3,  'member', now() - interval '50 days'),
    (3, 5,  'member', now() - interval '50 days'),
    (3, 8,  'member', now() - interval '52 days'),
    (3, 14, 'member', now() - interval '52 days'),
    (3, 15, 'viewer', now() - interval '40 days'),
    -- 等保整改：已收尾
    (4, 1,  'admin',  now() - interval '236 days'),
    (4, 11, 'admin',  now() - interval '236 days'),
    (4, 3,  'member', now() - interval '234 days'),
    (4, 4,  'member', now() - interval '234 days'),
    (4, 9,  'member', now() - interval '236 days'),
    (4, 13, 'member', now() - interval '230 days'),
    (4, 15, 'viewer', now() - interval '150 days');

-- ── O ────────────────────────────────────────────────────────────────────────
INSERT INTO objectives (id, project_id, title, description, sort_order, created_at)
OVERRIDING SYSTEM VALUE VALUES
    (1,  1, '核心库完成国产化替换，业务连续性不受影响', '三套核心库（交易、资金、对账）在年内完成替换，割接期间对外服务不中断。', 1, now() - interval '180 days'),
    (2,  1, '形成可复用的割接与回退方案',               '方案要能被其他系统直接套用，回退判定标准明确到分钟级。',               2, now() - interval '180 days'),
    (3,  1, '新库的运维与安全能力同步就位',             '不能出现「库迁完了但没人会运维」的情况。',                             3, now() - interval '176 days'),
    (4,  2, '工单与客户信息在中台完成统一',             '一期先收敛工单域，客户信息只做只读聚合。',                             1, now() - interval '110 days'),
    (5,  2, '一期具备可运营上线条件',                   'UAT 通过 + 坐席培训完成 + 运营手册交付，缺一不算达成。',               2, now() - interval '110 days'),
    (6,  2, '客服数据口径统一，支撑服务质量分析',       '先解决同一个指标三个部门三种口径的问题。',                             3, now() - interval '104 days'),
    (7,  3, '知识库在两条业务线完成试点验证',           '售后与投诉两条线，验证内容治理和检索效果两件事。',                     1, now() - interval '52 days'),
    (8,  3, '形成可推广的试点结论',                     '结论要能直接支撑是否扩大范围的决策。',                                 2, now() - interval '52 days'),
    (9,  4, '测评发现问题全部闭环',                     '46 项问题按高中低分级整改，高危项优先。',                             1, now() - interval '234 days'),
    (10, 4, '形成常态化安全运营机制',                   '整改不能是一次性的，要留下机制。',                                     2, now() - interval '234 days');

-- ── KR ───────────────────────────────────────────────────────────────────────
INSERT INTO key_results (id, objective_id, description, metric, owner_id, start_date, end_date, risk_level, sort_order, created_at)
OVERRIDING SYSTEM VALUE VALUES
    (1,  1, '完成三套核心库的兼容性评估与改造清单', '不兼容对象 100% 登记并给出改造方案',      3,  current_date - 175, current_date - 100, 'normal',    1, now() - interval '179 days'),
    (2,  1, '应用侧 SQL 与驱动完成适配并通过回归',   '核心交易回归用例通过率 ≥ 99%',            4,  current_date - 150, current_date - 20,  'warning',   2, now() - interval '179 days'),
    (3,  1, '完成两轮全量迁移演练，单轮窗口 ≤ 6 小时', '演练窗口 ≤ 6 小时且数据校验零差异',      9,  current_date - 120, current_date + 25,  'high_risk', 3, now() - interval '176 days'),
    (4,  2, '割接方案与回退预案通过公司级评审',     '方案评审通过，回退演练成功 1 次',          2,  current_date - 90,  current_date + 30,  'warning',   1, now() - interval '176 days'),
    (5,  2, '关键交易性能不劣化超过 10%',           '12 个关键交易迁移前后 TP99 对比达标',      6,  current_date - 80,  current_date + 40,  'normal',    2, now() - interval '172 days'),
    (6,  3, '完成新库备份恢复、监控告警与权限基线', '备份恢复演练通过，核心指标告警覆盖 100%',  11, current_date - 70,  current_date + 50,  'normal',    1, now() - interval '170 days'),
    (7,  4, '工单域模型与接口契约定稿',             '契约评审通过并冻结，变更走版本管理',       3,  current_date - 100, current_date - 40,  'normal',    1, now() - interval '108 days'),
    (8,  4, '中台服务完成开发与联调',               '对外接口可用率 ≥ 99.5%，联调用例全通过',   4,  current_date - 70,  current_date + 35,  'warning',   2, now() - interval '108 days'),
    (9,  4, '坐席工作台完成改版并通过可用性测试',   '关键操作路径步数下降 30%',                 5,  current_date - 60,  current_date + 45,  'normal',    3, now() - interval '106 days'),
    (10, 5, '三类业务场景 UAT 全部通过',            '售后、投诉、回访三类场景零阻塞缺陷',       12, current_date - 30,  current_date + 60,  'high_risk', 1, now() - interval '100 days'),
    (11, 5, '完成坐席培训与运营手册交付',           '两轮培训覆盖 120 名坐席，手册验收通过',    14, current_date - 20,  current_date + 70,  'normal',    2, now() - interval '96 days'),
    (12, 6, '客服指标口径统一并上线数据看板',       '18 个核心指标口径统一，看板日更',          8,  current_date - 45,  current_date + 55,  'warning',   1, now() - interval '96 days'),
    (13, 7, '知识条目治理规范落地，首批 500 条入库', '首批 500 条通过审校，返工率 < 10%',        12, current_date - 50,  current_date + 20,  'normal',    1, now() - interval '50 days'),
    (14, 7, '检索效果达到可用水平',                 'Top3 命中率 ≥ 85%',                        8,  current_date - 35,  current_date + 35,  'warning',   2, now() - interval '50 days'),
    (15, 8, '两条业务线完成 4 周试运行',            '试运行期间坐席日活 ≥ 60 人',               7,  current_date - 20,  current_date + 45,  'normal',    1, now() - interval '48 days'),
    (16, 8, '输出试点评估报告与推广建议',           '报告通过中心办公会评审',                   7,  current_date,       current_date + 60,  'normal',    2, now() - interval '48 days'),
    (17, 9, '46 项测评问题整改并复测通过',          '高危项 100% 闭环，复测无遗留高危',         11, current_date - 230, current_date - 70,  'normal',    1, now() - interval '233 days'),
    (18, 9, '日志审计与安全设备补齐',               '核心系统日志接入率 100%，留存 6 个月',     9,  current_date - 220, current_date - 90,  'normal',    2, now() - interval '233 days'),
    (19, 10,'建立季度自查与漏洞闭环机制',           '季度自查执行 1 次并形成问题台账',          11, current_date - 200, current_date - 60,  'normal',    1, now() - interval '230 days');
