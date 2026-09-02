-- 交付物项、交付内容（当前／候选）、交付物边与输入请求。
--
-- 当前内容（current）＝已生效，使下游关系就绪；候选内容（candidate）不提前满足输入。
-- 因此已完成任务的交付物挂 current，执行中任务多为 candidate 或还没有内容——
-- 下游任务的「等待输入」和上游未就绪卡点就是这么来的。

INSERT INTO deliverables (id, task_id, name, created_by, created_at)
OVERRIDING SYSTEM VALUE VALUES
(1,  1,  '核心库不兼容对象清单',       3,  now() - interval '176 days'),
(2,  2,  '存储过程改造清单与工作量评估', 4,  now() - interval '168 days'),
(3,  3,  '字符集与排序规则差异确认单',  9,  now() - interval '158 days'),
(4,  4,  '兼容性评估结论评审纪要',     3,  now() - interval '143 days'),
(5,  5,  '订单模块 SQL 改造对照表',     4,  now() - interval '138 days'),
(6,  6,  '资金模块驱动切换说明',       4,  now() - interval '96 days'),
(7,  6,  '连接池参数压测记录',         4,  now() - interval '60 days'),
(8,  7,  '对账 SQL 改写对照表',         3,  now() - interval '78 days'),
(9,  8,  '国产化适配回归测试报告',     6,  now() - interval '58 days'),
(10, 10, '全量迁移数据校验方案',       9,  now() - interval '113 days'),
(11, 11, '第一轮全量迁移演练报告',     9,  now() - interval '88 days'),
(12, 11, '第一轮演练问题跟踪表',       9,  now() - interval '86 days'),
(13, 12, '第二轮全量迁移演练报告',     9,  now() - interval '18 days'),
(14, 14, '核心库割接实施方案',         9,  now() - interval '58 days'),
(15, 14, '割接人员分工与联络表',       9,  now() - interval '40 days'),
(16, 15, '核心库迁移回退预案',         3,  now() - interval '53 days'),
(17, 18, '迁移前性能基线报告',         6,  now() - interval '76 days'),
(18, 19, '关键交易性能对比表',         6,  now() - interval '28 days'),
(19, 21, '备份恢复策略与演练记录',     9,  now() - interval '63 days'),
(20, 22, '新库监控项配置清单',         9,  now() - interval '48 days'),
(21, 23, '数据库账号权限台账',         11, now() - interval '9 days'),
(22, 24, '工单域模型与状态机说明',     3,  now() - interval '93 days'),
(23, 25, '客服中台一期接口契约',       3,  now() - interval '78 days'),
(24, 26, '历史工单字段映射表',         8,  now() - interval '68 days'),
(25, 26, '历史工单脏数据处理策略',     8,  now() - interval '60 days'),
(26, 27, '工单受理与流转服务接口文档', 4,  now() - interval '63 days'),
(27, 28, '客户信息聚合服务说明',       4,  now() - interval '48 days'),
(28, 29, '中台与呼叫中心联调记录',     6,  now() - interval '23 days'),
(29, 32, '坐席工作台交互改版方案',     10, now() - interval '53 days'),
(30, 32, '工作台关键路径走查记录',     10, now() - interval '45 days'),
(31, 33, '工作台前端组件说明',         5,  now() - interval '33 days'),
(32, 36, '客服中台 UAT 用例集',         12, now() - interval '26 days'),
(33, 36, 'UAT 验收标准说明',            12, now() - interval '20 days'),
(34, 40, '坐席操作手册',               14, now() - interval '11 days'),
(35, 43, '客服核心指标口径说明',       8,  now() - interval '40 days'),
(36, 44, '服务质量看板设计说明',       8,  now() - interval '20 days'),
(37, 46, '知识条目分类与撰写规范',     12, now() - interval '44 days'),
(38, 47, '首批知识条目入库清单',       14, now() - interval '28 days'),
(39, 49, '知识检索评测集与评分口径',   8,  now() - interval '31 days'),
(40, 50, '检索效果评测报告',           8,  now() - interval '16 days'),
(41, 52, '试运行部署记录',             5,  now() - interval '7 days'),
(42, 56, '主机与数据库加固记录',       9,  now() - interval '213 days'),
(43, 57, '身份鉴别与审计整改说明',     4,  now() - interval '203 days'),
(44, 58, '等保 2.0 三级测评复测结论',   11, now() - interval '158 days'),
(45, 58, '整改问题闭环台账',           11, now() - interval '156 days'),
(46, 59, '日志审计接入清单',           9,  now() - interval '208 days'),
(47, 60, '堡垒机运维审计上线报告',     13, now() - interval '193 days'),
(48, 61, '季度安全自查清单',           11, now() - interval '178 days'),
(49, 62, '首次季度自查报告',           11, now() - interval '148 days');

-- ── 交付内容 ──────────────────────────────────────────────────────────────────
-- object_key 先留空，插完后统一按 handler 的命名规则回填。
INSERT INTO deliverable_files (id, deliverable_id, state, file_name, file_type, file_size, object_key, uploaded_by, uploaded_at, effective_at)
OVERRIDING SYSTEM VALUE VALUES
(1,  1,  'current',   '核心库不兼容对象清单_v1.3.xlsx',           'xlsx', 498176,  '', 3,  now() - interval '159 days', now() - interval '157 days'),
(2,  2,  'current',   '存储过程改造清单与工作量评估_v2.0.xlsx',   'xlsx', 262144,  '', 4,  now() - interval '151 days', now() - interval '149 days'),
(3,  3,  'current',   '字符集与排序规则差异确认单.docx',           'docx', 86016,   '', 9,  now() - interval '141 days', now() - interval '139 days'),
(4,  4,  'current',   '兼容性评估结论评审纪要.docx',               'docx', 71680,   '', 3,  now() - interval '136 days', now() - interval '134 days'),
(5,  5,  'current',   '订单模块SQL改造对照表_v1.6.xlsx',           'xlsx', 348160,  '', 4,  now() - interval '111 days', now() - interval '109 days'),
(6,  6,  'candidate', '资金模块驱动切换说明_v0.4.docx',            'docx', 62464,   '', 4,  now() - interval '9 days',   NULL),
(7,  8,  'candidate', '对账SQL改写对照表_v0.6.xlsx',               'xlsx', 194560,  '', 3,  now() - interval '4 days',   NULL),
(8,  10, 'current',   '全量迁移数据校验方案_v1.2.docx',            'docx', 133120,  '', 9,  now() - interval '96 days',  now() - interval '94 days'),
(9,  11, 'current',   '第一轮全量迁移演练报告.docx',               'docx', 215040,  '', 9,  now() - interval '71 days',  now() - interval '69 days'),
(10, 12, 'current',   '第一轮演练问题跟踪表.xlsx',                 'xlsx', 118784,  '', 9,  now() - interval '71 days',  now() - interval '69 days'),
(11, 14, 'candidate', '核心库割接实施方案_V2.1（送审稿）.docx',     'docx', 428032,  '', 9,  now() - interval '6 days',   NULL),
(12, 15, 'candidate', '割接人员分工与联络表.xlsx',                 'xlsx', 45056,   '', 9,  now() - interval '6 days',   NULL),
(13, 16, 'candidate', '核心库迁移回退预案_V1.4.docx',              'docx', 176128,  '', 3,  now() - interval '3 days',   NULL),
(14, 17, 'current',   '迁移前性能基线报告.pdf',                    'pdf',  1572864, '', 6,  now() - interval '61 days',  now() - interval '59 days'),
(15, 18, 'candidate', '关键交易性能对比_第一批.xlsx',              'xlsx', 87040,   '', 6,  now() - interval '5 days',   NULL),
(16, 19, 'candidate', '备份恢复策略_v0.9.docx',                    'docx', 98304,   '', 9,  now() - interval '8 days',   NULL),
(17, 20, 'candidate', '新库监控项配置清单_v0.8.xlsx',              'xlsx', 76800,   '', 9,  now() - interval '6 days',   NULL),
(18, 22, 'current',   '工单域模型与状态机说明_v1.0.docx',          'docx', 245760,  '', 3,  now() - interval '76 days',  now() - interval '74 days'),
(19, 23, 'current',   '客服中台一期接口契约_v1.0.docx',            'docx', 671744,  '', 3,  now() - interval '56 days',  now() - interval '54 days'),
(20, 24, 'current',   '历史工单字段映射表_v1.1.xlsx',              'xlsx', 522240,  '', 8,  now() - interval '46 days',  now() - interval '44 days'),
(21, 25, 'current',   '历史工单脏数据处理策略.docx',               'docx', 105472,  '', 8,  now() - interval '46 days',  now() - interval '44 days'),
(22, 26, 'candidate', '工单受理与流转服务接口文档_v0.9.docx',      'docx', 312320,  '', 4,  now() - interval '2 days',   NULL),
(23, 29, 'current',   '坐席工作台交互改版方案_v2.0.pdf',           'pdf',  4194304, '', 10, now() - interval '39 days',  now() - interval '37 days'),
(24, 30, 'current',   '工作台关键路径走查记录.xlsx',               'xlsx', 63488,   '', 10, now() - interval '39 days',  now() - interval '37 days'),
(25, 31, 'candidate', '工作台前端组件说明_v0.7.docx',              'docx', 91136,   '', 5,  now() - interval '3 days',   NULL),
(26, 32, 'candidate', '客服中台UAT用例集_v0.8.xlsx',               'xlsx', 286720,  '', 12, now() - interval '2 days',   NULL),
(27, 34, 'candidate', '坐席操作手册_v0.5（初稿）.docx',             'docx', 1048576, '', 14, now() - interval '3 days',   NULL),
(28, 35, 'current',   '客服核心指标口径说明_v1.0.xlsx',            'xlsx', 158720,  '', 8,  now() - interval '26 days',  now() - interval '24 days'),
(29, 36, 'candidate', '服务质量看板设计说明_v0.6.docx',            'docx', 138240,  '', 8,  now() - interval '4 days',   NULL),
(30, 37, 'current',   '知识条目分类与撰写规范_v1.0.docx',          'docx', 187392,  '', 12, now() - interval '33 days',  now() - interval '31 days'),
(31, 38, 'candidate', '首批知识条目入库清单_0812.xlsx',            'xlsx', 409600,  '', 14, now() - interval '2 days',   NULL),
(32, 39, 'current',   '知识检索评测集_v1.0.xlsx',                  'xlsx', 231424,  '', 8,  now() - interval '21 days',  now() - interval '19 days'),
(33, 40, 'candidate', '检索效果评测报告_第一轮.docx',              'docx', 124928,  '', 8,  now() - interval '5 days',   NULL),
(34, 42, 'current',   '主机与数据库加固记录.xlsx',                 'xlsx', 174080,  '', 9,  now() - interval '191 days', now() - interval '189 days'),
(35, 43, 'current',   '身份鉴别与审计整改说明.docx',               'docx', 152576,  '', 4,  now() - interval '171 days', now() - interval '169 days'),
(36, 44, 'current',   '等保2.0三级测评复测结论.pdf',               'pdf',  2621440, '', 11, now() - interval '141 days', now() - interval '139 days'),
(37, 45, 'current',   '等保整改问题闭环台账.xlsx',                 'xlsx', 267264,  '', 11, now() - interval '141 days', now() - interval '139 days'),
(38, 46, 'current',   '日志审计接入清单.xlsx',                     'xlsx', 94208,   '', 9,  now() - interval '176 days', now() - interval '174 days'),
(39, 47, 'current',   '堡垒机运维审计上线报告.docx',               'docx', 143360,  '', 13, now() - interval '161 days', now() - interval '159 days'),
(40, 48, 'current',   '季度安全自查清单_v1.0.xlsx',                'xlsx', 82944,   '', 11, now() - interval '151 days', now() - interval '149 days'),
(41, 49, 'current',   '首次季度自查报告.docx',                     'docx', 167936,  '', 11, now() - interval '121 days', now() - interval '119 days'),
-- 报完成被退回后仍留在任务上的候选内容
(42, 9,  'candidate', '国产化适配回归测试报告_v0.3.docx',          'docx', 211968,  '', 6,  now() - interval '13 days',  NULL);

-- 与 handler 一致的对象键：deliverables/{交付物项 id}/{上传时刻纳秒}-{文件名}
UPDATE deliverable_files
SET object_key = 'deliverables/' || deliverable_id || '/'
              || (extract(epoch FROM uploaded_at) * 1000000000)::bigint || '-' || file_name;

-- ── 交付物边 ──────────────────────────────────────────────────────────────────
INSERT INTO deliverable_edges (id, target_task_id, source_task_id, source_user_id, name, necessity, expected_date, created_by, created_at)
OVERRIDING SYSTEM VALUE VALUES
-- 迁移项目
(1,  2,  1,    NULL,    '核心库不兼容对象清单',       'required',  current_date - 166, 3,  now() - interval '170 days'),
(2,  4,  1,    NULL,    '不兼容对象清单（评审输入）', 'reference', NULL,               3,  now() - interval '147 days'),
(3,  5,  2,    NULL,    '存储过程改造清单',           'required',  current_date - 138, 4,  now() - interval '142 days'),
(4,  6,  2,    NULL,    '存储过程改造清单',           'required',  current_date - 98,  4,  now() - interval '103 days'),
(5,  7,  3,    NULL,    '字符集与排序规则差异确认单', 'reference', NULL,               3,  now() - interval '83 days'),
(6,  8,  5,    NULL,    '订单模块 SQL 改造对照表',     'required',  current_date - 58,  6,  now() - interval '62 days'),
(7,  8,  6,    NULL,    '资金模块驱动切换说明',       'required',  current_date - 10,  6,  now() - interval '62 days'),
(8,  9,  NULL, 9, '新库测试环境连接信息与只读账号', 'required',  current_date + 3,   4,  now() - interval '12 days'),
(9,  11, 10,   NULL,   '全量迁移数据校验方案',       'required',  current_date - 88,  9,  now() - interval '92 days'),
(10, 12, 11,   NULL,   '第一轮迁移演练报告',         'required',  current_date - 18,  6,  now() - interval '26 days'),
(11, 13, 12,   NULL,   '第二轮迁移演练报告',         'required',  current_date + 8,   9,  now() - interval '18 days'),
(12, 14, 11,   NULL,   '第一轮演练暴露的问题',       'reference', NULL,               2,  now() - interval '63 days'),
(13, 16, 14,   NULL,   '割接实施方案（定稿）',       'required',  current_date + 6,   2,  now() - interval '30 days'),
(14, 17, 15,   NULL,   '回退预案（定稿）',           'required',  current_date + 10,  2,  now() - interval '28 days'),
(15, 19, 18,   NULL,   '迁移前性能基线报告',         'required',  current_date - 28,  6,  now() - interval '32 days'),
(16, 22, NULL, 11, '安全基线要求覆盖的监控项',   'reference', current_date - 30,  9,  now() - interval '50 days'),
-- 客服中台
(17, 25, 24,   NULL,   '工单域模型与状态机说明',     'required',  current_date - 78,  3,  now() - interval '82 days'),
(18, 26, 25,   NULL,   '接口契约（字段映射依据）',   'reference', NULL,               8,  now() - interval '72 days'),
(19, 27, 25,   NULL,   '工单接口契约',               'required',  current_date - 63,  4,  now() - interval '67 days'),
(20, 28, 25,   NULL,   '客户信息接口契约',           'required',  current_date - 48,  4,  now() - interval '52 days'),
(21, 29, 27,   NULL,   '工单受理服务（联调可用版本）', 'required', current_date - 5,  6,  now() - interval '27 days'),
(22, 29, 28,   NULL,   '客户信息聚合服务（联调可用版本）', 'required', current_date + 2, 6, now() - interval '27 days'),
(23, 33, 32,   NULL,   '工作台交互改版方案',         'required',  current_date - 33,  5,  now() - interval '37 days'),
(24, 34, 33,   NULL,   '工作台前端组件（可测版本）', 'required',  current_date + 8,   5,  now() - interval '20 days'),
(25, 36, 25,   NULL,   '接口契约（用例编写依据）',   'reference', NULL,               12, now() - interval '30 days'),
(26, 37, 29,   NULL,   '联调通过的中台环境',         'required',  current_date + 3,   12, now() - interval '22 days'),
(27, 37, 36,   NULL,   'UAT 用例集（售后场景）',      'required',  current_date + 3,   12, now() - interval '22 days'),
(28, 38, 36,   NULL,   'UAT 用例集（投诉场景）',      'required',  current_date + 6,   12, now() - interval '22 days'),
(29, 39, 37,   NULL, '售后场景 UAT 问题记录',       'reference', NULL,               12, now() - interval '20 days'),
(30, 40, 32,   NULL,   '工作台改版后的操作路径',     'reference', NULL,               14, now() - interval '14 days'),
(31, 41, 40,   NULL,   '坐席操作手册（培训版）',     'required',  current_date + 16,  14, now() - interval '16 days'),
(32, 44, 43,   NULL,   '客服核心指标口径说明',       'required',  current_date - 20,  8,  now() - interval '25 days'),
(33, 44, NULL, 12, '班组维度的服务质量口径确认', 'required',  current_date - 10,  8,  now() - interval '20 days'),
(34, 45, 44,   NULL,   '服务质量看板（试运行版本）', 'required',  current_date + 10,  8,  now() - interval '18 days'),
(35, 30, NULL, 3, '网关鉴权与限流方案确认',     'required',  current_date + 5,   4,  now() - interval '15 days'),
-- 知识库试点
(36, 47, 46,   NULL,   '知识条目撰写规范',           'required',  current_date - 28,  12, now() - interval '32 days'),
(37, 47, NULL, 12, '首批条目的业务口径复核意见', 'reference', current_date + 2,   14, now() - interval '9 days'),
(38, 50, 49,   NULL,   '检索评测集与评分口径',       'required',  current_date - 16,  8,  now() - interval '20 days'),
(39, 51, 50,   NULL,   '两轮评测结论',               'required',  current_date + 8,   8,  now() - interval '14 days'),
(40, 52, 47,   NULL,   '首批知识条目（可用版本）',   'required',  current_date - 2,   7,  now() - interval '10 days'),
(41, 53, 52,   NULL,   '试运行环境（两条业务线）',   'required',  current_date + 12,  7,  now() - interval '12 days'),
(42, 54, 53,   NULL, '坐席反馈与问题记录',         'required',  current_date + 32,  7,  now() - interval '10 days'),
-- 等保整改
(43, 58, 56,   NULL,   '主机与数据库加固记录',       'required',  current_date - 188, 11, now() - interval '162 days'),
(44, 58, 57,   NULL,   '身份鉴别与审计整改说明',     'required',  current_date - 168, 11, now() - interval '162 days'),
(45, 60, 59,   NULL,   '日志审计接入清单',           'required',  current_date - 173, 9,  now() - interval '198 days'),
(46, 62, 61,   NULL,   '季度安全自查清单',           'required',  current_date - 148, 11, now() - interval '152 days');

-- ── 输入请求（成员 → 任务 的那几条边）─────────────────────────────────────────
INSERT INTO input_requests (id, edge_id, provider_id, content_note, state, notified_at, accepted_at, provided_at, provided_text, file_name, object_key, created_at)
OVERRIDING SYSTEM VALUE VALUES
(1, 8,  9,  '需要新库测试环境的连接串、只读账号和白名单开通，报表工具要连过去跑兼容性用例。', 'pending',
    now() - interval '12 days', NULL, NULL, '', '', '', now() - interval '12 days'),
(2, 16, 11, '安全基线里要求必须监控的项，麻烦确认一下清单，我按这个配告警。', 'provided',
    now() - interval '50 days', now() - interval '49 days', now() - interval '46 days',
    '按等保整改的基线，数据库这边必须监控：特权账号登录、失败登录次数、审计日志写入失败、表空间使用率。前两项要留痕 6 个月。', '', '', now() - interval '50 days'),
(3, 33, 12, '看板要出班组维度，需要业务侧确认「一次解决率」在班组口径下怎么算，是否排除转派工单。', 'provided',
    now() - interval '20 days', now() - interval '20 days', now() - interval '16 days',
    '班组口径下一次解决率＝该班组首次接触即结案工单 ÷ 该班组受理工单，转派出去的不计入分母，转派进来的算接收班组。', '客服指标口径补充说明_班组维度.docx', '', now() - interval '20 days'),
(4, 35, 3,  '网关鉴权走统一 token 还是各服务自校验，限流按坐席工号还是按班组，需要架构给个结论。', 'pending',
    now() - interval '15 days', NULL, NULL, '', '', '', now() - interval '15 days'),
(5, 37, 12, '首批 300 条已经审校完，麻烦业务侧抽检 50 条看口径有没有跑偏。', 'accepted',
    now() - interval '9 days', now() - interval '8 days', NULL, '', '', '', now() - interval '9 days');

-- 输入请求附件的对象键同样按 handler 的规则拼：input-requests/{请求 id}/{提交时刻纳秒}-{文件名}
UPDATE input_requests
SET object_key = 'input-requests/' || id || '/' || (extract(epoch FROM provided_at) * 1000000000)::bigint || '-' || file_name
WHERE file_name <> '';
