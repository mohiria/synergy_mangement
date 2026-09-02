-- 完成申请（含中间或签快照）、关闭申请、任务创建邀请与提醒留痕。

INSERT INTO completion_reviews (id, task_id, submitted_by, note, state, opinion, submitted_at, decided_by, decided_at,
                                intermediate_by, intermediate_at, intermediate_opinion)
OVERRIDING SYSTEM VALUE VALUES
(1,  1,  3,  '三套库的对象清单已全量比对完，不兼容项 412 条全部标注了结论。', 'approved', '',
     now() - interval '159 days', 3,  now() - interval '157 days', NULL, NULL, ''),
(2,  2,  4,  '改造清单按模块拆到人，合计 86 人天。',                         'approved', '清单可以作为排期依据，工作量按这个报计划',
     now() - interval '151 days', 3,  now() - interval '149 days', NULL, NULL, ''),
(3,  3,  9,  '中文排序和空串处理两处差异已经和业务确认了影响面。',           'approved', '',
     now() - interval '141 days', 3,  now() - interval '139 days', NULL, NULL, ''),
(4,  4,  3,  '评审通过，结论纳入项目基线，纪要已同步全员。',                 'approved', '',
     now() - interval '136 days', 3,  now() - interval '134 days', NULL, NULL, ''),
(5,  5,  4,  '订单模块 214 处 SQL 改造完成，主流程回归全部通过。',            'approved', '',
     now() - interval '111 days', 4,  now() - interval '109 days', NULL, NULL, ''),
-- 8 号任务报过一次完成，被退回后回到进行中。
(6,  8,  6,  '两轮回归已经跑完第一轮，先报一次。',                           'rejected', '差异场景那一轮还没跑，通过率也没到 99%，跑完再报',
     now() - interval '12 days',  4,  now() - interval '10 days',  NULL, NULL, ''),
(7,  10, 9,  '校验方案含抽样比例与差异判定口径，DBA 与业务都确认过。',       'approved', '',
     now() - interval '96 days',  9,  now() - interval '94 days',  NULL, NULL, ''),
(8,  11, 9,  '第一轮演练完成，窗口 9 小时 20 分，问题清单一并提交。',        'approved', '窗口超了目标，第二轮重点压缩索引重建',
     now() - interval '71 days',  9,  now() - interval '69 days',  NULL, NULL, ''),
(9,  18, 6,  '12 个关键交易连续采集 5 个工作日，基线报告已出。',              'approved', '',
     now() - interval '61 days',  6,  now() - interval '59 days',  NULL, NULL, ''),
(10, 24, 3,  '状态机从 13 个状态收敛到 7 个，产品和业务都确认了。',           'approved', '',
     now() - interval '76 days',  3,  now() - interval '74 days',  NULL, NULL, ''),
(11, 25, 3,  '一期 23 个接口契约评审通过，版本冻结为 v1.0。',                 'approved', '冻结后变更走版本管理，不再直接改文档',
     now() - interval '56 days',  3,  now() - interval '54 days',  NULL, NULL, ''),
(12, 26, 8,  '380 万条历史工单的字段映射与脏数据分布都出来了。',             'approved', '映射表和脏数据策略都齐了',
     now() - interval '46 days',  3,  now() - interval '44 days',  NULL, NULL, ''),
(13, 32, 10, '改版方案通过可用性走查，关键路径从 9 步降到 6 步。',           'approved', '',
     now() - interval '39 days',  5,  now() - interval '37 days',  NULL, NULL, ''),
(14, 43, 8,  '18 个指标口径统一，首次解决率的三套算法已经收敛成一套。',      'approved', '',
     now() - interval '26 days',  8,  now() - interval '24 days',  NULL, NULL, ''),
(15, 46, 12, '规范经两名资深坐席试写验证，可执行。',                         'approved', '',
     now() - interval '33 days',  12, now() - interval '31 days',  NULL, NULL, ''),
(16, 49, 8,  '评测集 300 条来自真实坐席提问，评分口径产品已确认。',          'approved', '',
     now() - interval '21 days',  8,  now() - interval '19 days',  NULL, NULL, ''),
(17, 56, 9,  '23 项高危配置全部整改并通过复扫。',                             'approved', '',
     now() - interval '191 days', 11, now() - interval '189 days', NULL, NULL, ''),
(18, 57, 4,  '6 个业务系统的口令策略、双因素和操作审计均已满足要求。',       'approved', '',
     now() - interval '171 days', 11, now() - interval '169 days', NULL, NULL, ''),
(19, 58, 11, '测评机构复测通过，46 项问题全部闭环，无遗留高危。',            'approved', '',
     now() - interval '141 days', 11, now() - interval '139 days', NULL, NULL, ''),
(20, 59, 9,  '日志审计设备到位，12 个核心系统接入完成，留存 6 个月。',       'approved', '',
     now() - interval '176 days', 9,  now() - interval '174 days', NULL, NULL, ''),
(21, 60, 13, '运维直连通道已关闭，操作录像可回溯。',                         'approved', '',
     now() - interval '161 days', 9,  now() - interval '159 days', NULL, NULL, ''),
(22, 61, 11, '自查清单 58 项，责任到岗位，办公会已通过。',                   'approved', '',
     now() - interval '151 days', 11, now() - interval '149 days', NULL, NULL, ''),
(23, 62, 11, '首次自查 11 个问题全部闭环。',                                 'approved', '',
     now() - interval '121 days', 11, now() - interval '119 days', NULL, NULL, ''),
-- 14 号任务：中间或签已通过，停在 KR 终审 5 天（审批超时卡点）。
(24, 14, 9,  '割接方案 V2.1 送审稿，含时序、分工、通信机制与观察指标；分工联络表一并提交。',
     'pending_final', '', now() - interval '6 days', NULL, NULL,
     3, now() - interval '5 days', '方案结构没问题，割接时序第三步和第四步的并行关系再确认一下'),
-- 15 号任务：还在中间或签，尚未到超时阈值。
(25, 15, 3,  '回退预案 V1.4，判定标准细化到分钟级，请审核组过一遍。',
     'intermediate_review', '', now() - interval '2 days', NULL, NULL, NULL, NULL, ''),
-- 47 号任务：报完成被退回，回到进行中继续返工。
(26, 47, 14, '已入库 312 条，先报一次阶段成果。',
     'rejected', '摘要过长的问题还没改完，先按规范返工，另外 500 条没入完不算完成',
     now() - interval '9 days', 12, now() - interval '7 days', NULL, NULL, '');

-- 完成申请里的交付物与文件名快照（候选被覆盖后审核记录仍可追溯）
INSERT INTO completion_review_items (review_id, deliverable_id, deliverable_name, file_name, file_id) VALUES
(1,  1,  '核心库不兼容对象清单',       '核心库不兼容对象清单_v1.3.xlsx',         1),
(2,  2,  '存储过程改造清单与工作量评估', '存储过程改造清单与工作量评估_v2.0.xlsx', 2),
(3,  3,  '字符集与排序规则差异确认单',  '字符集与排序规则差异确认单.docx',        3),
(4,  4,  '兼容性评估结论评审纪要',     '兼容性评估结论评审纪要.docx',            4),
(5,  5,  '订单模块 SQL 改造对照表',     '订单模块SQL改造对照表_v1.6.xlsx',        5),
(6,  9,  '国产化适配回归测试报告',     '国产化适配回归测试报告_v0.3.docx',       42),
(7,  10, '全量迁移数据校验方案',       '全量迁移数据校验方案_v1.2.docx',         8),
(8,  11, '第一轮全量迁移演练报告',     '第一轮全量迁移演练报告.docx',            9),
(8,  12, '第一轮演练问题跟踪表',       '第一轮演练问题跟踪表.xlsx',              10),
(9,  17, '迁移前性能基线报告',         '迁移前性能基线报告.pdf',                 14),
(10, 22, '工单域模型与状态机说明',     '工单域模型与状态机说明_v1.0.docx',       18),
(11, 23, '客服中台一期接口契约',       '客服中台一期接口契约_v1.0.docx',         19),
(12, 24, '历史工单字段映射表',         '历史工单字段映射表_v1.1.xlsx',           20),
(12, 25, '历史工单脏数据处理策略',     '历史工单脏数据处理策略.docx',            21),
(13, 29, '坐席工作台交互改版方案',     '坐席工作台交互改版方案_v2.0.pdf',        23),
(13, 30, '工作台关键路径走查记录',     '工作台关键路径走查记录.xlsx',            24),
(14, 35, '客服核心指标口径说明',       '客服核心指标口径说明_v1.0.xlsx',         28),
(15, 37, '知识条目分类与撰写规范',     '知识条目分类与撰写规范_v1.0.docx',       30),
(16, 39, '知识检索评测集与评分口径',   '知识检索评测集_v1.0.xlsx',               32),
(17, 42, '主机与数据库加固记录',       '主机与数据库加固记录.xlsx',              34),
(18, 43, '身份鉴别与审计整改说明',     '身份鉴别与审计整改说明.docx',            35),
(19, 44, '等保 2.0 三级测评复测结论',   '等保2.0三级测评复测结论.pdf',            36),
(19, 45, '整改问题闭环台账',           '等保整改问题闭环台账.xlsx',              37),
(20, 46, '日志审计接入清单',           '日志审计接入清单.xlsx',                  38),
(21, 47, '堡垒机运维审计上线报告',     '堡垒机运维审计上线报告.docx',            39),
(22, 48, '季度安全自查清单',           '季度安全自查清单_v1.0.xlsx',             40),
(23, 49, '首次季度自查报告',           '首次季度自查报告.docx',                  41),
(24, 14, '核心库割接实施方案',         '核心库割接实施方案_V2.1（送审稿）.docx', 11),
(24, 15, '割接人员分工与联络表',       '割接人员分工与联络表.xlsx',              12),
(25, 16, '核心库迁移回退预案',         '核心库迁移回退预案_V1.4.docx',           13),
(26, 38, '首批知识条目入库清单',       '首批知识条目入库清单_0812.xlsx',         31);

-- 提交时的中间审核组快照
INSERT INTO completion_review_reviewers (review_id, user_id) VALUES
    (24, 3), (24, 11),
    (25, 11), (25, 6),
    (26, 12);

-- ── 关闭申请（裁决 #172：变更类审批只剩关闭申请，表沿用 field_change_requests）──
INSERT INTO field_change_requests (id, task_id, submitted_by, reason, state, exempt, opinion, resolved,
                                   change_type, old_status, new_status,
                                   submitted_at, decided_by, decided_at)
OVERRIDING SYSTEM VALUE VALUES
-- 退回且未处理：会出现在提交人的「我的工作 · 待我处理」
(5, 8, 6, '回归范围并入国产化差异专项统一验证，本任务不再单独执行', 'rejected', FALSE,
    '差异场景专项只覆盖用例梳理，回归执行仍要本任务收口，继续执行', FALSE,
    'cancel', 'in_progress', 'cancelled',
    now() - interval '6 days', 4, now() - interval '4 days'),
-- 停了 4 天没人审：审批超时卡点
(6, 7, 3, '对账比对改用采购的专用工具完成，脚本改写不再需要', 'pending', FALSE, '', FALSE,
    'cancel', 'in_progress', 'cancelled',
    now() - interval '4 days', NULL, NULL),
(7, 52, 5, '投诉线并入工作台联调整体验证，本任务关闭', 'pending', FALSE, '', FALSE,
    'cancel', 'not_started', 'cancelled',
    now() - interval '1 day', NULL, NULL);

-- ── 任务创建邀请 ──────────────────────────────────────────────────────────────
INSERT INTO task_invites (id, key_result_id, inviter_id, invitee_id, note, state, created_at)
OVERRIDING SYSTEM VALUE VALUES
(1, 3,  9,  6,  '演练问题清单这块你熟，麻烦你建个任务跟到底。',           'completed', now() - interval '19 days'),
(2, 6,  11, 9,  '备份恢复和监控告警两块请你建任务，按等保基线来。',       'completed', now() - interval '69 days'),
(3, 9,  5,  10, '工作台改版的可用性测试由你牵头，任务你自己建一下。',     'pending',   now() - interval '5 days'),
(4, 12, 8,  14, '看板试运行阶段的坐席反馈收集，麻烦你建任务跟一下。',     'pending',   now() - interval '3 days'),
(5, 10, 12, 14, '投诉场景 UAT 你来组织，先建任务。',                     'revoked',   now() - interval '24 days');

-- ── 一键提醒留痕（冷却按发起人、被提醒人、任务三元组计次）────────────────────
-- recipient_id 与 05_collab.sql 的 blocker_remind 通知一一对应。
INSERT INTO remind_logs (id, task_id, sender_id, recipient_id, target_key, remind_date, created_at)
OVERRIDING SYSTEM VALUE VALUES
(1, 6,  2,  4,  'task_overdue:6',                    current_date - 1, now() - interval '1 day'  + interval '2 hours'),
(2, 22, 11, 9,  'task_overdue:22',                   current_date,     now() - interval '5 hours'),
(3, 29, 12, 4,  'upstream_unready:edge:21',          current_date - 1, now() - interval '1 day'  + interval '4 hours'),
-- 裁决 #178：输入请求提醒目标（wait:input_request:*）随机制退场删除。
(5, 14, 2,  2,  'approval_timeout:final_review:24',  current_date - 2, now() - interval '2 days' + interval '3 hours');
