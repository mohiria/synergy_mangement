(function () {
  const members = [
    ["P01", "林岚", "项目总负责人", "项目决策", "lead"], ["P02", "陈越", "总推进人", "项目统筹", "admin"],
    ["P03", "周宁", "KR负责人", "体验设计", "kr"], ["P04", "宋佳", "KR负责人", "内容运营", "kr"],
    ["P05", "许晨", "KR负责人", "观众服务", "kr"], ["P06", "顾言", "任务负责人", "体验设计", "member"],
    ["P07", "唐可", "KR负责人", "运行保障", "kr"], ["P08", "韩松", "KR负责人", "现场运行", "kr"],
    ["P09", "姜雨", "KR负责人", "安全保障", "kr"], ["P10", "陆川", "中间审核人", "专业评审", "member"],
    ["P11", "沈悦", "KR负责人", "内容发布", "kr"], ["P12", "方圆", "KR负责人", "成果管理", "kr"],
    ["P13", "罗静", "KR负责人", "数据复盘", "kr"], ["P14", "苏航", "普通参与人", "空间协作", "member"],
    ["P15", "叶青", "中间审核人", "体验评审", "member"], ["P16", "吴桐", "普通参与人", "设备支持", "member"],
    ["P17", "程野", "普通参与人", "现场执行", "member"], ["P18", "夏安", "只读成员", "观察组", "readonly"],
  ].map(([id, displayName, role, team, permission]) => ({ id, displayName, role, team, permission }));

  const objectives = [
    { id: "O1", title: "形成完整、可感知的未来生活体验路径", note: "从到场、探索到离场形成连贯体验" },
    { id: "O2", title: "确保体验周稳定、安全并按计划运行", note: "场地、人员与服务在高峰期保持稳定" },
    { id: "O3", title: "沉淀可复用的内容资产与复盘成果", note: "让阶段成果能够追溯、复用和交接" },
  ];
  const krs = [
    ["KR1", "完成四条主题体验动线并通过联调", "O1", "P03", "warning", "08.20—09.18", "4 条动线全部可连续体验"],
    ["KR2", "建立互动内容与现场装置的联动机制", "O1", "P04", "normal", "08.20—09.18", "12 个互动点响应稳定"],
    ["KR3", "形成覆盖全周期的观众服务方案", "O1", "P05", "risk", "08.20—09.18", "关键服务触点覆盖率 100%"],
    ["KR4", "完成场地、设备与网络的联合保障", "O2", "P07", "warning", "08.20—09.18", "核心系统可用率不低于 99%"],
    ["KR5", "建立分时分区的现场运行机制", "O2", "P08", "normal", "08.20—09.18", "高峰期平均等待低于 12 分钟"],
    ["KR6", "完成安全预案与两轮全流程演练", "O2", "P09", "risk", "08.20—09.18", "重点场景演练覆盖率 100%"],
    ["KR7", "形成统一的内容发布与传播素材库", "O3", "P11", "normal", "08.27—09.25", "核心渠道素材齐套率 100%"],
    ["KR8", "完成阶段成果、过程文件与版本归集", "O3", "P12", "warning", "08.27—09.25", "正式成果来源可追溯"],
    ["KR9", "建立数据采集、反馈与复盘闭环", "O3", "P13", "normal", "08.27—09.25", "形成 3 类反馈分析结论"],
    ["KR10", "形成项目收尾成果包与改进路线", "O3", "P03", "risk", "08.27—09.25", "形成 3 个固定版本成果包"],
  ].map(([id, title, objectiveId, owner, risk, cycle, metric]) => ({ id, title, objectiveId, owner, risk, cycle, metric }));

  const taskSubjects = [
    ["体验触点清单", "主题动线基线", "互动脚本", "全链路走查"], ["装置接口约定", "内容触发规则", "异常回退方案", "现场联动测试"],
    ["观众分层规则", "服务触点手册", "咨询话术包", "服务情景演练"], ["场地资源台账", "设备部署方案", "网络保障清单", "联合压力测试"],
    ["分时预约规则", "现场排班表", "峰值疏导方案", "运行沙盘演练"], ["风险场景清单", "应急处置卡", "人员响应机制", "全流程安全演练"],
    ["内容发布节奏", "渠道素材规范", "核心视觉素材", "发布前齐套检查"], ["成果分类规则", "过程文件索引", "当前内容生效清单", "阶段归集检查"],
    ["数据采集口径", "反馈问卷与访谈", "运行数据分析", "综合复盘纪要"], ["成果包目录", "阶段版本固化", "复盘问题台账", "改进路线确认"],
  ];
  const actionWords = ["梳理", "确认", "编制", "联调"];
  const specialStatuses = { T01: "待入池审批", T02: "待中间审核", T09: "待 KR 终审", T24: "已完成", T32: "已完成", T40: "已完成" };
  const configuredReviewers = { T02: ["P10", "P15"], T14: ["P10"], T23: ["P15"], T31: ["P10", "P15"] };
  const primaryOwners = ["P06", "P14", "P16", "P17", "P06", "P14", "P16", "P17", "P06", "P14"];
  const tasks = [];
  krs.forEach((kr, krIndex) => taskSubjects[krIndex].forEach((subject, taskIndex) => {
    const number = krIndex * 4 + taskIndex + 1, id = `T${String(number).padStart(2, "0")}`;
    const status = specialStatuses[id] || ["进行中", "等待输入", "未开始", "进行中"][number % 4];
    const owner = ["T02", "T03"].includes(id) ? "P06" : taskIndex === 0 ? primaryOwners[krIndex] : `P${String(6 + ((number + taskIndex) % 12)).padStart(2, "0")}`;
    const title = `${actionWords[taskIndex]}${subject}`;
    tasks.push({ id, krId: kr.id, title, owner, participants: [`P${String(6 + (number % 10)).padStart(2, "0")}`, `P${String(7 + (number % 9)).padStart(2, "0")}`],
      status, progress: status === "已完成" ? 100 : [null, 25, 45, 70][number % 4], start: `09-${String(1 + (number % 14)).padStart(2, "0")}`,
      due: `09-${String(12 + (number % 15)).padStart(2, "0")}`, outputName: `${subject}成果稿`, receiver: kr.owner,
      middleReviewers: configuredReviewers[id] || [],
      description: `围绕“${kr.title}”形成可执行、可追溯的阶段结果。`, createdBy: number % 2 ? "P02" : "P06",
      updatedAt: `09-${String(8 + (number % 9)).padStart(2, "0")} ${String(9 + (number % 8)).padStart(2, "0")}:20` });
  }));

  const relationPairs = [];
  krs.forEach((kr, index) => { const start = index * 4 + 1; for (let i = 0; i < 3; i += 1) relationPairs.push([start+i,start+i+1,"hard","成果输入",index<2?"CP-A":index===5||index===7?"CP-B":null]); });
  [[4,5],[8,13],[12,17],[16,21],[20,25],[24,29],[28,33],[32,37],[2,11],[10,19],[18,31],[23,36],[6,27],[15,34],[26,39],[9,30]].forEach(([from,to],i)=>relationPairs.push([from,to,"hard",i%2?"确认后启动":"正式输入",from===4&&to===5?"CP-A":from===24&&to===29?"CP-B":null]));
  [[7,14],[14,7],[25,33],[33,25]].forEach(([from,to])=>relationPairs.push([from,to,"interlock","互锁条件",null]));
  [[11,3],[22,14],[31,23],[40,32]].forEach(([from,to])=>relationPairs.push([from,to,"feedback","迭代反馈",null]));
  [[1,18],[5,22],[13,30],[17,38],[29,40],[4,21]].forEach(([from,to])=>relationPairs.push([from,to,"input","参考输入",null]));
  const relations = relationPairs.map(([fromNumber,toNumber,type,label,pathId],index)=>({ id:`R${String(index+1).padStart(2,"0")}`,from:`T${String(fromNumber).padStart(2,"0")}`,to:`T${String(toNumber).padStart(2,"0")}`,type,label,pathId,necessity:type === "input" ? "参考" : "必要",state:index%9===0?"等待当前交付物生效":"已就绪" }));

  const deliverables = Array.from({ length: 22 }, (_, index) => { const task = tasks[index%tasks.length], state=[1,8].includes(index)?"审核中":"已生效", formedAt=`09-${String(9+(index%10)).padStart(2,"0")} 16:30`; return {
    id:`D${String(index+1).padStart(2,"0")}`,krId:task.krId,taskId:task.id,relationId:relations[index%relations.length].id,name:task.outputName,owner:task.owner,submittedBy:task.owner,receiver:task.receiver,
    state,formedAt,file:`future-life-${String(index+1).padStart(2,"0")}.pdf`,fileType:"PDF",fileSize:`${1.2+(index%6)*0.4} MB`,submittedAt:formedAt,effectiveAt:state==="已生效"?formedAt:"" }; });
  deliverables.push(
    {id:"D23",krId:"KR1",taskId:"T02",relationId:"R02",name:tasks[1].outputName,owner:"P06",submittedBy:"P06",receiver:"P03",state:"已生效",formedAt:"09-10 14:20",file:"theme-route-baseline.pdf",fileType:"PDF",fileSize:"2.4 MB",submittedAt:"09-10 13:40",effectiveAt:"09-10 14:20"},
    {id:"D24",krId:"KR3",taskId:"T09",relationId:"R09",name:tasks[8].outputName,owner:"P14",submittedBy:"P14",receiver:"P05",state:"已生效",formedAt:"09-12 17:10",file:"audience-service-rules.docx",fileType:"Word",fileSize:"860 KB",submittedAt:"09-12 16:25",effectiveAt:"09-12 17:10"},
    {id:"D25",krId:"KR1",taskId:"T02",relationId:"R02",name:"主题动线联调说明",owner:"P06",submittedBy:"P06",receiver:"P03",state:"审核中",formedAt:"昨天 18:10",file:"theme-route-review-notes.docx",fileType:"Word",fileSize:"740 KB",submittedAt:"昨天 18:10",effectiveAt:""}
  );

  const entryApprovals = [{id:"EA01",taskId:"T01",submitter:"P14",krOwner:"P03",state:"待审批",reason:"补齐体验触点基线的前置梳理",createdAt:"09-14 09:00",waitingDays:4},{id:"EA02",taskId:"T19",submitter:"P14",krOwner:"P08",state:"待审批",reason:"新增高峰疏导验证任务",createdAt:"昨天 15:20",waitingDays:1}];
  const changeRequests = [{id:"CR01",taskId:"T03",submitter:"P06",krOwner:"P03",state:"待审批",field:"截止时间",oldValue:"09-15",newValue:"09-18",reason:"互动脚本联调窗口后移",createdAt:"09-16 10:30",waitingDays:2},{id:"CR02",taskId:"T06",submitter:"P06",krOwner:"P04",state:"已退回",field:"输入来源",oldValue:"装置接口约定成果稿",newValue:"现场触发规则清单",reason:"希望改用联调现场确认稿",opinion:"请补充新来源的责任人和预计就绪时间",handledAt:"今天 09:10",waitingDays:1,dismissed:false}];
  const completionApprovals = [
    {id:"CA01",taskId:"T02",submitter:"P06",krOwner:"P03",state:"中间审核中",createdAt:"昨天 18:10",waitingDays:1,deliverableIds:["D02","D25"],candidateSnapshot:["D02","D25"].map((id)=>{const item=deliverables.find((x)=>x.id===id);return {id:item.id,name:item.name,file:item.file,fileType:item.fileType,fileSize:item.fileSize};}),reviewers:[{person:"P10",state:"待审核"},{person:"P15",state:"待审核"}]},
    {id:"CA02",taskId:"T09",submitter:"P14",krOwner:"P05",state:"待 KR 终审",createdAt:"09-14 09:15",waitingDays:4,deliverableIds:["D09"],candidateSnapshot:["D09"].map((id)=>{const item=deliverables.find((x)=>x.id===id);return {id:item.id,name:item.name,file:item.file,fileType:item.fileType,fileSize:item.fileSize};}),reviewers:[]}
  ];
  const inputRequests = [{id:"IR01",taskId:"T08",inputName:"互动装置点位确认表",provider:"P14",requester:"P06",state:"待接收",due:"09-18",createdAt:"09-15 11:00",waitingDays:3,necessity:"必要",impact:"影响现场联动测试和 2 个下游任务"},{id:"IR02",taskId:"T16",inputName:"场地峰值流量预估",provider:"P06",requester:"P07",state:"已接收",due:"09-20",createdAt:"09-16 14:00",waitingDays:2,necessity:"必要",impact:"影响联合压力测试"},{id:"IR03",taskId:"T30",inputName:"传播口径参考资料",provider:"P06",requester:"P13",state:"待接收",due:"09-22",createdAt:"今天 08:30",waitingDays:0,necessity:"参考",impact:"仅用于完善表达，不阻塞任务"}];
  const risks = [
    {id:"B01",taskId:"T01",level:"预警",reason:"任务等待入池审批",actionOwner:"P03",days:1,impact:"影响主题动线基线"}, {id:"B02",taskId:"T11",level:"高风险",reason:"观众服务接口输入尚未确认",actionOwner:"P05",days:4,impact:"影响 4 个服务触点任务"},
    {id:"B03",taskId:"T14",level:"预警",reason:"场地窗口尚未确认",actionOwner:"P07",days:2,impact:"影响设备部署与联调"}, {id:"B04",taskId:"T23",level:"高风险",reason:"两项硬依赖形成互锁",actionOwner:"P09",days:3,impact:"影响 2 条关键路径"},
    {id:"B05",taskId:"T31",level:"预警",reason:"候选内容等待专业确认",actionOwner:"P10",days:2,impact:"影响阶段成果归集"}, {id:"B06",taskId:"T38",level:"高风险",reason:"成果目录缺少已生效当前内容",actionOwner:"P12",days:1,impact:"影响收尾成果包 V1.0"},
  ];
  const packages = [{id:"PK01",name:"体验内容基线成果包",scope:"O1 / 阶段一",version:"V1.0",formedAt:"09-10 18:00",owner:"P02",deliverableIds:["D01","D23","D03","D05","D06"]},{id:"PK02",name:"联合保障演练成果包",scope:"O2 / 阶段二",version:"V1.1",formedAt:"09-16 20:30",owner:"P07",deliverableIds:["D07","D08","D24","D10","D11","D12"]},{id:"PK03",name:"项目收尾与复盘成果包",scope:"O3 / 收尾",version:"V0.8",formedAt:"09-20 11:00",owner:"P12",deliverableIds:["D13","D14","D15","D16","D17","D18"]}];

  const discussions = [
    {id:"DC01",taskId:"T01",author:"P01",content:"请在入池前补充体验触点基线的范围说明，@ 周宁 帮忙核对。",mentions:["P03"],createdAt:"今天 09:35"},
    {id:"DC02",taskId:"T01",author:"P14",content:"现场观察纪录已补齐，建议将排队等待也纳入触点清单。",mentions:[],createdAt:"今天 10:02"},
    {id:"DC03",taskId:"T02",author:"P10",content:"候选内容整体可用，请 @ 顾言 再确认两处动线切换说明。",mentions:["P06"],createdAt:"昨天 19:10"}
  ];

  const taskInvites = [{id:"TI01",krId:"KR2",inviter:"P04",invitee:"P06",note:"请补充一项现场联动异常回退验证任务",state:"待处理",createdAt:"09-17 10:20",waitingDays:1}];
  const reminders = [];

  window.PROTOTYPE_SEED = { project:{id:"PJT-FLX",name:"未来生活科技体验周筹备项目",phase:"联合联调阶段",cycle:"2026.08.20—2026.09.25",lead:"P01",coordinator:"P02",approvalTimeoutDays:3},members,objectives,krs,tasks,relations,deliverables,entryApprovals,changeRequests,completionApprovals,inputRequests,risks,packages,discussions,taskInvites,reminders,
    audit:[{time:"今天 10:26",actor:"P02",action:"提醒 P03 处理 T01 待周宁审批"},{time:"今天 09:40",actor:"P10",action:"打开 T02 完成申请"},{time:"昨天 18:10",actor:"P06",action:"提交 D02、D25 候选交付物"},{time:"昨天 16:22",actor:"P03",action:"查看关系图谱中的互锁路径"}] };
})();
