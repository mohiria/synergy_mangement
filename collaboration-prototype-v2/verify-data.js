/* eslint-disable no-console */
const fs = require("fs");
const vm = require("vm");
const source = fs.readFileSync(require("path").join(__dirname, "data.js"), "utf8");
const sandbox = { window: {} };
vm.createContext(sandbox);
vm.runInContext(source, sandbox);
const d = sandbox.window.PROTOTYPE_SEED;
const assert = (condition, message) => { if (!condition) throw new Error(message); };

assert(d.objectives.length >= 3, "需要至少 3 个 O");
assert(d.krs.length >= 10, "需要至少 10 个 KR");
assert(d.tasks.length >= 40, "需要足够任务验证层级与关系");
assert(d.relations.length >= 50, "需要足够关系验证复杂图谱");
assert(d.relations.filter((x) => x.type === "interlock").length >= 4, "需要两组双向互锁");
assert(d.relations.filter((x) => x.type === "feedback").length >= 3, "需要反馈循环");
assert(d.deliverables.length >= 15, "需要至少 15 项正式成果");
assert(d.deliverables.every((item) => !("versions" in item) && !("currentVersion" in item)), "交付物不得保留版本历史字段");
assert(d.deliverables.some((item) => item.state === "审核中") && d.deliverables.some((item) => item.state === "已生效"), "需要同时演示当前与候选内容");
assert(d.completionApprovals.some((item) => item.deliverableIds?.length > 1), "需要演示多项候选交付物整体审核");
assert(d.discussions?.length >= 2, "需要任务讨论演示数据");
assert(d.tasks.filter((task) => task.middleReviewers?.length).length === 4, "需要 4 个配置中间审核人的任务");
assert(d.members.every((x) => /^P\d{2}$/.test(x.id)), "成员必须使用脱敏代号");
assert(d.packages.length >= 2, "需要阶段成果包样例");
assert(d.entryApprovals.some((x) => x.krOwner === "P03"), "P03 需要可演示入池审批");
assert(d.changeRequests.some((x) => x.krOwner === "P03"), "P03 需要可演示关键变更审批");
assert(d.completionApprovals.some((x) => x.krOwner === "P03"), "P03 需要可演示最终闭环");
assert(d.completionApprovals.some((item) => item.reviewers.length >= 2), "需要多人或签完成审批");
console.log(`数据验证通过：${d.objectives.length} O / ${d.krs.length} KR / ${d.tasks.length} 任务 / ${d.relations.length} 关系 / ${d.deliverables.length} 交付物`);
