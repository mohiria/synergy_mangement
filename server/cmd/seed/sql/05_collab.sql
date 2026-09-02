-- 任务讨论、站内通知与任务动态。
--
-- 通知和动态不手写：按 handler 里定型的文案规则从已有事实生成，避免种子数据与代码跑偏。
-- 卡点类动态（卡点出现／解除）不在这里补——服务进程的每小时 ticker 与写操作 diff 会自己记。

INSERT INTO discussions (id, task_id, author_id, content, created_at)
OVERRIDING SYSTEM VALUE VALUES
(1,  6,  2,  '资金模块这块窗口只剩不到两周了，连接回收的问题定位到哪一步了？',                                   now() - interval '9 days'),
(2,  6,  4,  '定位到是长事务没提交导致连接被占住，改成显式提交后压测没再复现，今天再跑一轮 200 并发确认。',       now() - interval '9 days' + interval '2 hours'),
(3,  6,  9,  '压测环境的连接数上限我调到 600 了，别再被环境本身卡住。',                                           now() - interval '8 days'),
(4,  7,  3,  '对账脚本里有 37 处用到了原库的 DECODE 和 NVL2，改写成 CASE 之后要逐条比对结果，进度会慢一些。',      now() - interval '12 days'),
(5,  7,  4,  '@孙鹏 帮忙把对账的历史结果集导一份出来做比对基线，不然改一条验一条太慢。',                          now() - interval '11 days'),
(6,  7,  6,  '导好了，在测试机 /data/compare 下按日期分了目录，最近 30 天的都有。',                               now() - interval '11 days' + interval '3 hours'),
(7,  8,  4,  '回归这轮先跑核心交易，差异场景本来就在范围里，不用单独改任务名。',                                   now() - interval '5 days'),
(8,  8,  6,  '明白，那我按原来的完成标准补差异场景用例，下周三给第一轮结果。',                                     now() - interval '4 days'),
(9,  12, 9,  '第二轮演练目标 6 小时，现在拆下来导入 3 小时、索引重建 2 小时 40 分，余量太薄。',                     now() - interval '6 days'),
(10, 12, 6,  '索引重建能不能挑几个非核心表放到割接后异步建？',                                                     now() - interval '6 days' + interval '1 hour'),
(11, 12, 9,  '可以，但要确认这几张表割接当天不查历史数据。@陈牧阳 麻烦你从业务侧确认一下。',                       now() - interval '5 days'),
(12, 14, 3,  '方案里第三步和第四步写的是并行，但两步都要独占数据库，实际做不到，建议改成串行并把时长加上去。',     now() - interval '5 days'),
(13, 14, 9,  '收到，串行后总时长 5 小时 40 分，还在窗口内，我改一版再送审。',                                       now() - interval '5 days' + interval '2 hours'),
(14, 14, 9,  '@李建辉 终审这边麻烦尽快，公司级评审会已经定在下周二了。',                                           now() - interval '1 day'),
(15, 15, 11, '回退判定标准里「数据校验失败」要写清楚是抽样失败还是全量失败，现场没时间讨论这个。',                 now() - interval '1 day'),
(16, 19, 6,  '账户查询劣化 14%，看执行计划是走了全表扫，缺一个组合索引。',                                         now() - interval '4 days'),
(17, 19, 4,  '组合索引我在测试库先加上了，麻烦重新跑一轮对比。',                                                   now() - interval '3 days'),
(18, 21, 11, '异地恢复演练什么时候安排？等保复查那边要留证据。',                                                   now() - interval '7 days'),
(19, 21, 9,  '定在下周二晚上做，恢复窗口预计 2 小时，@徐帅 到时候一起盯一下。',                                     now() - interval '6 days'),
(20, 22, 11, '监控插件到货了没有？告警覆盖率一直卡在 60%。',                                                       now() - interval '2 days'),
(21, 22, 9,  '采购说下周三到货，到了当天我就配上，慢查询和锁等待两类先手工盯着。',                                 now() - interval '2 days' + interval '2 hours'),
(22, 23, 9,  '权限台账初稿出来了，共享账号有 9 个，建议全部收回改成个人账号加审批领用。',                           now() - interval '5 days'),
(23, 27, 6,  '联调环境的受理接口今天开始返回 500，是不是又发版了？',                                               now() - interval '3 days'),
(24, 27, 4,  '刚发了一版改流转状态的，已经回滚，现在恢复正常了。',                                                 now() - interval '3 days' + interval '1 hour'),
(25, 29, 6,  '呼叫中心侧的弹屏取数还在用挡板，受理接口稳定之前真联调开不了。',                                     now() - interval '2 days'),
(26, 33, 5,  '详情抽屉字段太多，建议按业务优先级折叠，@韩萌 看下交互稿要不要跟着调。',                             now() - interval '4 days'),
(27, 33, 10, '折叠没问题，但常用的四个字段要固定展示，我出个标注版给你。',                                         now() - interval '3 days'),
(28, 36, 12, '用例评审提了 11 条修改意见，主要集中在验收标准写得太模糊。',                                         now() - interval '3 days'),
(29, 40, 14, '手册要按新工作台重写，截图得等前端组件定版，我先把文字部分写完。',                                   now() - interval '5 days'),
(30, 44, 8,  '班组维度的口径郭婷已经确认了，看板这边按「转派出去的不计入分母」改。',                               now() - interval '10 days'),
(31, 47, 12, '摘要控制在 80 字以内，超长的那批麻烦返工，别直接把正文首段复制过去。',                               now() - interval '6 days'),
(32, 47, 14, '已经返工 60 条，剩下的这周处理完，后面新写的按规范来。',                                             now() - interval '2 days'),
(33, 48, 12, '流程里要写清楚谁来复核，不然过期条目还是没人管。',                                                   now() - interval '1 day'),
(34, 50, 8,  '第一轮 Top3 命中率 76%，同义词和口语化提问是主要失分点，下一轮先加同义词表。',                        now() - interval '4 days'),
(35, 52, 5,  '售后线已经部署完了，投诉线要等工作台联调完成，这周先不动。',                                         now() - interval '2 days');

INSERT INTO discussion_mentions (discussion_id, user_id) VALUES
    (5,  6),
    (11, 3),
    (14, 2),
    (19, 11),
    (26, 10);

-- ── 站内通知 ──────────────────────────────────────────────────────────────────
-- 讨论通知：任务负责人 + 被 @ 的成员，不含作者本人（AC-36 的口径）。
INSERT INTO notifications (user_id, kind, content, project_id, task_id, created_at)
SELECT m.user_id,
       'discussion_mention',
       u.display_name || ' 在任务「' || t.name || '」的讨论中提到了你',
       o.project_id, t.id, d.created_at
FROM discussion_mentions m
JOIN discussions d ON d.id = m.discussion_id
JOIN users u ON u.id = d.author_id
JOIN tasks t ON t.id = d.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE m.user_id <> d.author_id;

INSERT INTO notifications (user_id, kind, content, project_id, task_id, created_at)
SELECT t.owner_id,
       'discussion_owner',
       u.display_name || ' 在任务「' || t.name || '」的讨论中发表了意见',
       o.project_id, t.id, d.created_at
FROM discussions d
JOIN users u ON u.id = d.author_id
JOIN tasks t ON t.id = d.task_id
JOIN key_results k ON k.id = t.key_result_id
JOIN objectives o ON o.id = k.objective_id
WHERE t.owner_id <> d.author_id
  AND NOT EXISTS (SELECT 1 FROM discussion_mentions m WHERE m.discussion_id = d.id AND m.user_id = t.owner_id);

-- 裁决 #178：输入请求机制退场，输入请求通知随之删除。

-- 一键提醒通知：与 remind_logs 一一对应，文案同 domain.RemindContent。
INSERT INTO notifications (user_id, kind, content, project_id, task_id, created_at) VALUES
(4,  'blocker_remind',
     '任务「完成资金模块驱动切换与连接池调优」提醒：缺「按期完成任务」（截止时间 '
     || to_char(current_date - 3, 'YYYY-MM-DD') || ' 已过，任务仍未完成）；截止 '
     || to_char(current_date - 3, 'YYYY-MM-DD') || '；沿硬前置影响下游 1 项任务：补齐适配后的回归用例并跑通两轮',
     1, 6,  now() - interval '1 day' + interval '2 hours'),
(9,  'blocker_remind',
     '任务「配置新库监控告警与巡检脚本」提醒：缺「按期完成任务」（截止时间 '
     || to_char(current_date - 1, 'YYYY-MM-DD') || ' 已过，任务仍未完成）；截止 '
     || to_char(current_date - 1, 'YYYY-MM-DD'),
     1, 22, now() - interval '5 hours'),
(4,  'blocker_remind',
     '任务「完成中台与呼叫中心系统联调」提醒：缺「工单受理服务（联调可用版本）」（上游任务「开发工单受理与流转服务」尚未交付当前内容）；截止 '
     || to_char(current_date + 15, 'YYYY-MM-DD') || '；沿硬前置影响下游 2 项任务：汇总 UAT 问题并跟踪闭环、组织售后场景 UAT',
     2, 29, now() - interval '1 day' + interval '4 hours'),
(2,  'blocker_remind',
     '任务「编制割接实施方案」提醒：缺「KR 终审处理」（KR 终审已等待 3 天，超过阈值 3 天）；截止 '
     || to_char(current_date + 2, 'YYYY-MM-DD') || '；沿硬前置影响下游 1 项任务：组织割接方案公司级评审',
     1, 14, now() - interval '2 days' + interval '3 hours');

-- 半个月前的通知都已读；最近的留一批未读。
UPDATE notifications SET read_at = created_at + interval '4 hours' WHERE created_at < now() - interval '15 days';
UPDATE notifications SET read_at = created_at + interval '2 hours'
WHERE read_at IS NULL AND created_at < now() - interval '5 days' AND id % 3 <> 0;

-- ── 任务动态 ──────────────────────────────────────────────────────────────────
-- 入池（裁决 #162）：创建即入池，每个任务一条「任务入池」。
INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at)
SELECT id, 'pool_entered', created_by, '任务入池', created_at
FROM tasks;

-- 完成申请：提交、中间或签通过、终审处理。
INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at)
SELECT task_id, 'completion_submitted', submitted_by, '提交完成申请：' || note, submitted_at
FROM completion_reviews;

INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at)
SELECT task_id, 'completion_approved', intermediate_by,
       '完成审核通过' || CASE WHEN intermediate_opinion <> '' THEN '：' || intermediate_opinion ELSE '' END,
       intermediate_at
FROM completion_reviews WHERE intermediate_at IS NOT NULL;

INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at)
SELECT task_id, 'completion_approved', decided_by,
       '完成审核通过' || CASE WHEN opinion <> '' THEN '：' || opinion ELSE '' END, decided_at
FROM completion_reviews WHERE state = 'approved' AND decided_at IS NOT NULL;

INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at)
SELECT task_id, 'completion_rejected', decided_by, '完成审核退回：' || opinion, decided_at
FROM completion_reviews WHERE state = 'rejected' AND decided_at IS NOT NULL;

-- 任务字段直接修改留痕（裁决 #172：立即生效、动态记「任务字段修改」）。
INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at) VALUES
(12, 'field_edited', 6, '任务字段修改：截止时间', now() - interval '14 days'),
(22, 'field_edited', 9, '任务字段修改：截止时间', now() - interval '22 days'),
(27, 'field_edited', 4, '任务字段修改：完成标准', now() - interval '11 days');

-- 任务关闭留痕（裁决 10，#180：项目管理员直接关闭、写任务动态）。
INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at)
SELECT id, 'task_closed', 7, '任务关闭：' || cancel_reason, updated_at
FROM tasks WHERE status = 'cancelled';

-- 接收确认
INSERT INTO task_activities (task_id, kind, actor_id, summary, occurred_at)
SELECT r.task_id, 'receipt_confirmed', r.user_id, '确认接收：' || u.display_name, r.confirmed_at
FROM task_receipts r JOIN users u ON u.id = r.user_id
WHERE r.confirmed_at IS NOT NULL;
